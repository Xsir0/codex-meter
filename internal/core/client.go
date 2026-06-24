package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultUsageURL        = "https://chatgpt.com/backend-api/wham/usage"
	DefaultResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	defaultMaxBodySize     = 4 << 20 // 4 MiB
)

type Client struct {
	HTTPClient      *http.Client
	UsageURL        string
	ResetCreditsURL string
	UserAgent       string
	MaxAttempts     int
	MaxBodySize     int64
}

type Snapshot struct {
	FetchedAt            time.Time
	Usage                *UsageResponse
	ResetCredits         *ResetCreditsResponse
	UsageRaw             json.RawMessage
	ResetCreditsRaw      json.RawMessage
	UsageError           error
	ResetCreditsError    error
	UsageDuration        time.Duration
	ResetCreditsDuration time.Duration
}

func (s Snapshot) Complete() bool {
	return s.UsageError == nil && s.ResetCreditsError == nil && s.Usage != nil && s.ResetCredits != nil
}

func (s Snapshot) Partial() bool {
	hasData := s.Usage != nil || s.ResetCredits != nil
	hasError := s.UsageError != nil || s.ResetCreditsError != nil
	return hasData && hasError
}

func (s Snapshot) Failed() bool {
	return s.Usage == nil && s.ResetCredits == nil
}

type APIError struct {
	Endpoint   string
	StatusCode int
	Kind       string
	Message    string
	RetryAfter time.Duration
	BodyHint   string
	Cause      error
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if e.Message != "" {
		parts = append(parts, e.Message)
	} else if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", e.StatusCode))
	} else {
		parts = append(parts, "request failed")
	}
	if e.RetryAfter > 0 {
		parts = append(parts, "retry in "+HumanDuration(e.RetryAfter))
	}
	if e.BodyHint != "" {
		parts = append(parts, "response: "+e.BodyHint)
	}
	if e.Cause != nil && e.Message == "" {
		parts = append(parts, e.Cause.Error())
	}
	return strings.Join(parts, "; ")
}

func (e *APIError) Unwrap() error { return e.Cause }

func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &Client{
		HTTPClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("too many redirects")
				}
				if len(via) == 0 {
					return nil
				}
				original := via[0].URL
				if request.URL.Scheme != original.Scheme || !strings.EqualFold(request.URL.Host, original.Host) {
					return fmt.Errorf("refusing to send Codex credentials to redirect target %s", request.URL.Redacted())
				}
				return nil
			},
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          10,
				IdleConnTimeout:       30 * time.Second,
				TLSHandshakeTimeout:   6 * time.Second,
				ResponseHeaderTimeout: timeout,
			},
		},
		UsageURL:        DefaultUsageURL,
		ResetCreditsURL: DefaultResetCreditsURL,
		UserAgent:       "codex-meter/2",
		MaxAttempts:     2,
		MaxBodySize:     defaultMaxBodySize,
	}
}

// FetchAll queries usage and reset credits concurrently. Each result is kept
// independently, so one broken endpoint does not hide valid data from the
// other endpoint.
func (c *Client) FetchAll(ctx context.Context, auth *AuthConfig) Snapshot {
	snapshot := Snapshot{FetchedAt: time.Now()}
	if auth == nil {
		err := errors.New("missing login credentials")
		snapshot.UsageError = err
		snapshot.ResetCreditsError = err
		return snapshot
	}

	var wait sync.WaitGroup
	wait.Add(2)

	go func() {
		defer wait.Done()
		started := time.Now()
		var response UsageResponse
		raw, err := c.fetchJSON(ctx, c.UsageURL, auth, &response)
		snapshot.UsageDuration = time.Since(started)
		if err != nil {
			snapshot.UsageError = err
			return
		}
		snapshot.Usage = &response
		snapshot.UsageRaw = raw
	}()

	go func() {
		defer wait.Done()
		started := time.Now()
		var response ResetCreditsResponse
		raw, err := c.fetchJSON(ctx, c.ResetCreditsURL, auth, &response)
		snapshot.ResetCreditsDuration = time.Since(started)
		if err != nil {
			snapshot.ResetCreditsError = err
			return
		}
		snapshot.ResetCredits = &response
		snapshot.ResetCreditsRaw = raw
	}()

	wait.Wait()
	return snapshot
}

func (c *Client) fetchJSON(ctx context.Context, endpoint string, auth *AuthConfig, destination any) (json.RawMessage, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, &APIError{Endpoint: endpoint, Kind: "configuration", Message: "endpoint URL is empty"}
	}
	if c.HTTPClient == nil {
		c.HTTPClient = NewClient(12 * time.Second).HTTPClient
	}
	attempts := c.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	maxBody := c.MaxBodySize
	if maxBody <= 0 {
		maxBody = defaultMaxBodySize
	}

	var lastError error
	for attempt := 1; attempt <= attempts; attempt++ {
		raw, retryAfter, err := c.fetchJSONOnce(ctx, endpoint, auth, destination, maxBody)
		if err == nil {
			return raw, nil
		}
		lastError = err
		if attempt >= attempts || !isRetryable(err) {
			break
		}
		delay := retryDelay(attempt, retryAfter)
		if delay > 2*time.Second {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, &APIError{Endpoint: endpoint, Kind: "cancelled", Message: "request was cancelled", Cause: ctx.Err()}
		case <-timer.C:
		}
	}
	return nil, lastError
}

func (c *Client) fetchJSONOnce(ctx context.Context, endpoint string, auth *AuthConfig, destination any, maxBody int64) (json.RawMessage, time.Duration, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, &APIError{Endpoint: endpoint, Kind: "configuration", Message: "could not create request", Cause: err}
	}
	request.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	if strings.TrimSpace(auth.AccountID) != "" {
		request.Header.Set("ChatGPT-Account-Id", auth.AccountID)
	}
	request.Header.Set("originator", "Codex Desktop")
	request.Header.Set("OAI-Product-Sku", "CODEX")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", firstNonEmpty(c.UserAgent, "codex-meter/2"))

	response, err := c.HTTPClient.Do(request)
	if err != nil {
		kind, message := classifyTransportError(err)
		return nil, 0, &APIError{Endpoint: endpoint, Kind: kind, Message: message, Cause: err}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return nil, 0, &APIError{Endpoint: endpoint, StatusCode: response.StatusCode, Kind: "read", Message: "could not read the server response", Cause: err}
	}
	if int64(len(body)) > maxBody {
		return nil, 0, &APIError{Endpoint: endpoint, StatusCode: response.StatusCode, Kind: "oversized", Message: fmt.Sprintf("server response exceeds the %s safety limit", humanBytes(maxBody))}
	}

	retryAfter := parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, retryAfter, statusError(endpoint, response.StatusCode, retryAfter, body)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, 0, &APIError{Endpoint: endpoint, StatusCode: response.StatusCode, Kind: "empty", Message: "server returned an empty response"}
	}

	contentType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if contentType != "" {
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr == nil && mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") {
			if !looksLikeJSON(body) {
				return nil, 0, &APIError{
					Endpoint: endpoint, StatusCode: response.StatusCode, Kind: "content_type",
					Message:  "server did not return JSON (Content-Type: " + contentType + ")",
					BodyHint: safeBodyHint(body),
				}
			}
		}
	} else if !looksLikeJSON(body) {
		return nil, 0, &APIError{Endpoint: endpoint, StatusCode: response.StatusCode, Kind: "content_type", Message: "server did not return JSON", BodyHint: safeBodyHint(body)}
	}

	if err := json.Unmarshal(body, destination); err != nil {
		return nil, 0, &APIError{
			Endpoint: endpoint, StatusCode: response.StatusCode, Kind: "decode",
			Message: "server JSON could not be decoded (the internal API may have changed)", BodyHint: safeBodyHint(body), Cause: err,
		}
	}
	return append(json.RawMessage(nil), body...), 0, nil
}

func statusError(endpoint string, status int, retryAfter time.Duration, body []byte) error {
	apiError := &APIError{
		Endpoint: endpoint, StatusCode: status, RetryAfter: retryAfter,
		Kind: "http", BodyHint: safeBodyHint(body),
	}
	switch status {
	case http.StatusUnauthorized:
		apiError.Kind = "unauthorized"
		apiError.Message = "login credentials are invalid or access_token has expired; run codex login and try again"
		apiError.BodyHint = ""
	case http.StatusForbidden:
		apiError.Kind = "forbidden"
		apiError.Message = "this account is not authorized to access Codex usage; verify the ChatGPT account and Codex access"
	case http.StatusNotFound:
		apiError.Kind = "not_found"
		apiError.Message = "the internal Codex endpoint was not found and may have changed"
	case http.StatusTooManyRequests:
		apiError.Kind = "rate_limited"
		apiError.Message = "too many requests; the service rate-limited this client"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		apiError.Kind = "server_unavailable"
		apiError.Message = "the Codex usage service is temporarily unavailable"
	default:
		if status >= 500 {
			apiError.Kind = "server"
			apiError.Message = fmt.Sprintf("the Codex usage service returned HTTP %d", status)
		} else {
			apiError.Message = fmt.Sprintf("the Codex usage endpoint returned HTTP %d", status)
		}
	}
	return apiError
}

func classifyTransportError(err error) (kind, message string) {
	if strings.Contains(err.Error(), "refusing to send Codex credentials to redirect target") {
		return "unsafe_redirect", "the server attempted a cross-origin redirect; the request was stopped to protect login credentials"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled", "request was cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", "request timed out; check the network and proxy settings, or increase --timeout"
	}
	var netError net.Error
	if errors.As(err, &netError) {
		if netError.Timeout() {
			return "timeout", "network connection timed out; check the network and proxy settings, or increase --timeout"
		}
		if netError.Temporary() {
			return "network_temporary", "temporary network error"
		}
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return "dns", "DNS lookup failed; check the network or proxy settings"
	}
	return "network", "could not connect to the Codex usage service; check the network and HTTPS proxy settings"
}

func isRetryable(err error) bool {
	var apiError *APIError
	if !errors.As(err, &apiError) {
		return false
	}
	if apiError.StatusCode == http.StatusTooManyRequests || apiError.StatusCode >= 500 {
		return true
	}
	switch apiError.Kind {
	case "timeout", "network", "network_temporary", "dns", "server", "server_unavailable":
		return true
	default:
		return false
	}
}

func retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	if attempt <= 1 {
		return 250 * time.Millisecond
	}
	return time.Duration(attempt) * 500 * time.Millisecond
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if timestamp, err := http.ParseTime(value); err == nil {
		if delay := timestamp.Sub(now); delay > 0 {
			return delay
		}
	}
	return 0
}

func looksLikeJSON(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
}

func safeBodyHint(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	// Avoid accidentally printing token-like values returned by an upstream
	// proxy or debug page.
	for _, prefix := range []string{"Bearer ", "access_token", "refresh_token", "id_token"} {
		if strings.Contains(strings.ToLower(trimmed), strings.ToLower(prefix)) {
			return "<response contained sensitive fields and was hidden>"
		}
	}
	runes := []rune(trimmed)
	if len(runes) > 180 {
		trimmed = string(runes[:180]) + "…"
	}
	return trimmed
}
