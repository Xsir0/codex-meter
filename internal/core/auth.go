package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const maxAuthFileSize = 2 << 20 // 2 MiB

// AuthConfig contains only the credential material needed for requests. The
// access token is deliberately excluded from JSON output and String methods.
type AuthConfig struct {
	Path        string
	AuthMode    string
	AccessToken string
	AccountID   string
	LastRefresh string
	Token       TokenMetadata
	Warnings    []string
}

// TokenMetadata is decoded from JWT claims without validating the signature.
// The server still performs the authoritative token validation.
type TokenMetadata struct {
	ExpiresAt time.Time
	IssuedAt  time.Time
	Subject   string
	Email     string
	PlanType  string
}

type authFilePayload struct {
	AuthMode      string `json:"auth_mode"`
	OpenAIAPIKey  string `json:"OPENAI_API_KEY"`
	AccessToken   string `json:"access_token"`
	AccountID     string `json:"account_id"`
	LastRefresh   string `json:"last_refresh"`
	CredentialTag string `json:"credential_type"`
	Tokens        struct {
		AccessToken  string `json:"access_token"`
		AccountID    string `json:"account_id"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"tokens"`
}

// ResolveAuthPath follows Codex's CODEX_HOME convention while allowing an
// explicit auth file to take precedence.
func ResolveAuthPath(explicitFile, explicitHome string) (string, error) {
	if strings.TrimSpace(explicitFile) != "" {
		return cleanPath(explicitFile)
	}

	home := strings.TrimSpace(explicitHome)
	if home == "" {
		home = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not determine the user home directory: %w", err)
		}
		home = filepath.Join(userHome, ".codex")
	} else {
		var err error
		home, err = cleanPath(home)
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(home, "auth.json"), nil
}

func cleanPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path cannot be empty")
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not expand ~: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("could not normalize path %q: %w", path, err)
	}
	return absolute, nil
}

func LoadAuth(path string, now time.Time) (*AuthConfig, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("Codex credentials were not found at %s; run codex login first or specify auth.json with --auth-file", path)
		}
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("permission denied while reading Codex credentials at %s: %w", path, err)
		}
		return nil, fmt.Errorf("could not inspect Codex credentials at %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Codex credentials path is not a regular file: %s", path)
	}
	if info.Size() <= 0 {
		return nil, fmt.Errorf("Codex credentials file is empty: %s", path)
	}
	if info.Size() > maxAuthFileSize {
		return nil, fmt.Errorf("Codex credentials file is unexpectedly large (%s; limit 2 MiB)", humanBytes(info.Size()))
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open Codex credentials at %s: %w", path, err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxAuthFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("could not read Codex credentials at %s: %w", path, err)
	}
	if len(data) > maxAuthFileSize {
		return nil, fmt.Errorf("Codex credentials exceed the 2 MiB limit: %s", path)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	var payload authFilePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("Codex credentials are not valid JSON at %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("Codex credentials contain multiple JSON values: %s", path)
		}
		return nil, fmt.Errorf("Codex credentials contain invalid trailing data at %s: %w", path, err)
	}

	accessToken := firstNonEmpty(payload.Tokens.AccessToken, payload.AccessToken)
	if accessToken == "" {
		if strings.TrimSpace(payload.OpenAIAPIKey) != "" {
			return nil, fmt.Errorf("%s contains only an API key, not a ChatGPT OAuth access_token; this tool reads ChatGPT Codex usage, so run codex login with a ChatGPT account", path)
		}
		return nil, fmt.Errorf("%s does not contain tokens.access_token; run codex login again", path)
	}
	if strings.ContainsAny(accessToken, "\r\n\t ") {
		return nil, fmt.Errorf("the access_token in %s has an invalid format; run codex login again", path)
	}

	claims, claimErr := decodeJWTClaims(accessToken)
	metadata := tokenMetadataFromClaims(claims)
	accountID := firstNonEmpty(
		claimString(claims, "https://api.openai.com/auth.chatgpt_account_id"),
		claimNestedString(claims, "https://api.openai.com/auth", "chatgpt_account_id"),
		claimString(claims, "chatgpt_account_id"),
		payload.Tokens.AccountID,
		payload.AccountID,
	)
	auth := &AuthConfig{
		Path:        path,
		AuthMode:    firstNonEmpty(payload.AuthMode, payload.CredentialTag, "chatgpt"),
		AccessToken: accessToken,
		AccountID:   accountID,
		LastRefresh: payload.LastRefresh,
		Token:       metadata,
	}
	if claimErr != nil {
		auth.Warnings = append(auth.Warnings, "access_token is not a parseable JWT, so its expiration cannot be displayed; the server will still validate it")
	}
	if accountID == "" {
		auth.Warnings = append(auth.Warnings, "ChatGPT Account ID could not be parsed, so the ChatGPT-Account-Id header will not be sent; the server will determine whether this login is usable")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		auth.Warnings = append(auth.Warnings, fmt.Sprintf("credentials file permissions are %04o; consider running chmod 600 %q", info.Mode().Perm(), path))
	}
	if !metadata.ExpiresAt.IsZero() {
		remaining := metadata.ExpiresAt.Sub(now)
		switch {
		case remaining <= 0:
			auth.Warnings = append(auth.Warnings, "access_token has expired; if the request returns 401, run codex login to refresh the login")
		case remaining <= 15*time.Minute:
			auth.Warnings = append(auth.Warnings, "access_token will expire within 15 minutes")
		}
	}
	return auth, nil
}

func decodeJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return nil, errors.New("JWT does not contain enough segments")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some producers retain base64 padding.
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("could not decode JWT payload as base64: %w", err)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var claims map[string]any
	if err := decoder.Decode(&claims); err != nil {
		return nil, fmt.Errorf("could not decode JWT payload JSON: %w", err)
	}
	return claims, nil
}

func tokenMetadataFromClaims(claims map[string]any) TokenMetadata {
	var metadata TokenMetadata
	if unix, ok := claimUnix(claims, "exp"); ok {
		metadata.ExpiresAt = time.Unix(unix, 0)
	}
	if unix, ok := claimUnix(claims, "iat"); ok {
		metadata.IssuedAt = time.Unix(unix, 0)
	}
	metadata.Subject = claimString(claims, "sub")
	metadata.Email = firstNonEmpty(
		claimString(claims, "email"),
		claimNestedString(claims, "https://api.openai.com/profile", "email"),
	)
	metadata.PlanType = firstNonEmpty(
		claimString(claims, "https://api.openai.com/auth.chatgpt_plan_type"),
		claimNestedString(claims, "https://api.openai.com/auth", "chatgpt_plan_type"),
		claimString(claims, "chatgpt_plan_type"),
	)
	return metadata
}

func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	value, ok := claims[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ""
}

func claimNestedString(claims map[string]any, objectKey, fieldKey string) string {
	if claims == nil {
		return ""
	}
	object, ok := claims[objectKey].(map[string]any)
	if !ok {
		return ""
	}
	value, ok := object[fieldKey]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ""
}

func claimUnix(claims map[string]any, key string) (int64, bool) {
	if claims == nil {
		return 0, false
	}
	value, ok := claims[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case json.Number:
		if number, err := typed.Int64(); err == nil {
			return number, true
		}
		if number, err := strconv.ParseFloat(typed.String(), 64); err == nil {
			return int64(number), true
		}
	case float64:
		return int64(typed), true
	case string:
		if number, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return number, true
		}
	}
	return 0, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func MaskIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "—"
	}
	runes := []rune(value)
	if len(runes) <= 8 {
		if len(runes) <= 2 {
			return strings.Repeat("•", len(runes))
		}
		return string(runes[:1]) + strings.Repeat("•", len(runes)-2) + string(runes[len(runes)-1:])
	}
	return string(runes[:4]) + "…" + string(runes[len(runes)-4:])
}

func MaskEmail(email string) string {
	email = strings.TrimSpace(email)
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" {
		return MaskIdentifier(email)
	}
	local := []rune(parts[0])
	visible := string(local[:1])
	if len(local) > 1 {
		visible += strings.Repeat("•", minInt(len(local)-1, 6))
	}
	return visible + "@" + parts[1]
}

func humanBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	divisor, exponent := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(divisor), "KMGTPE"[exponent])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
