package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchAllSuccessAndHeaders(t *testing.T) {
	var usageSeen, creditsSeen atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization header = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("ChatGPT-Account-Id") != "acct-123" {
			t.Errorf("account header = %q", request.Header.Get("ChatGPT-Account-Id"))
		}
		if request.Header.Get("originator") != "Codex Desktop" || request.Header.Get("OAI-Product-Sku") != "CODEX" {
			t.Errorf("missing Codex headers: %v", request.Header)
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/usage":
			usageSeen.Store(true)
			_, _ = writer.Write([]byte(`{
				"plan_type":"pro",
				"rate_limit":{"allowed":true,"limit_reached":false,
				"primary_window":{"used_percent":25,"limit_window_seconds":18000,"reset_after_seconds":1200},
				"secondary_window":{"used_percent":"40","limit_window_seconds":604800,"reset_at":1782306000000}},
				"additional_rate_limits":[
					{"limit_name":"GPT-5.3-Codex-Spark","metered_feature":"codex_bengalfox","rate_limit":{"primary_window":{"used_percent":15,"limit_window_seconds":18000}}},
					{"limit_name":"broken","rate_limit":"not-an-object"}
				],
				"new_top_field":{"enabled":true}
			}`))
		case "/credits":
			creditsSeen.Store(true)
			_, _ = writer.Write([]byte(`{
				"credits":[
					{"id":"credit-1","status":"available","reset_type":"rate_limit","expires_at":"2026-06-28T12:00:00Z","title":"Weekly reset"},
					{"status":"available"}
				],
				"server_field":"kept"
			}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(2 * time.Second)
	client.UsageURL = server.URL + "/usage"
	client.ResetCreditsURL = server.URL + "/credits"
	client.MaxAttempts = 1
	snapshot := client.FetchAll(context.Background(), &AuthConfig{AccessToken: "test-token", AccountID: "acct-123"})

	if !snapshot.Complete() || !usageSeen.Load() || !creditsSeen.Load() {
		t.Fatalf("incomplete snapshot: %+v", snapshot)
	}
	if snapshot.Usage.PlanType != "pro" {
		t.Fatalf("plan = %q", snapshot.Usage.PlanType)
	}
	remaining, ok := snapshot.Usage.RateLimit.PrimaryWindow.RemainingPercent()
	if !ok || remaining != 75 {
		t.Fatalf("remaining = %d, %v", remaining, ok)
	}
	if snapshot.ResetCredits.AvailableCount != 1 || snapshot.ResetCredits.DroppedCredits != 1 {
		t.Fatalf("credits = %+v", snapshot.ResetCredits)
	}
	if len(snapshot.Usage.AdditionalRateLimits) != 1 || !snapshot.Usage.AdditionalRateLimits[0].IsSpark() {
		t.Fatalf("Spark quota was not decoded independently: %+v", snapshot.Usage.AdditionalRateLimits)
	}
	if snapshot.Usage.Extra["new_top_field"] == nil || snapshot.ResetCredits.Extra["server_field"] == nil {
		t.Fatal("unknown fields were not retained")
	}
}

func TestFetchAllKeepsPartialSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/usage" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":10}}}`))
			return
		}
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte(`<html><body>gateway page</body></html>`))
	}))
	defer server.Close()

	client := NewClient(time.Second)
	client.UsageURL = server.URL + "/usage"
	client.ResetCreditsURL = server.URL + "/credits"
	client.MaxAttempts = 1
	snapshot := client.FetchAll(context.Background(), &AuthConfig{AccessToken: "token", AccountID: "account"})

	if !snapshot.Partial() || snapshot.Usage == nil || snapshot.ResetCredits != nil {
		t.Fatalf("expected partial success: %+v", snapshot)
	}
	var apiError *APIError
	if !errors.As(snapshot.ResetCreditsError, &apiError) || apiError.Kind != "content_type" {
		t.Fatalf("unexpected credits error: %#v", snapshot.ResetCreditsError)
	}
}

func TestFetchRetriesTransientServerError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(writer, "temporary", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"plan_type":"pro"}`))
	}))
	defer server.Close()

	client := NewClient(2 * time.Second)
	client.MaxAttempts = 2
	var response UsageResponse
	_, err := client.fetchJSON(context.Background(), server.URL, &AuthConfig{AccessToken: "token", AccountID: "account"}, &response)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestRateLimitErrorPreservesRetryAfterWithoutLongSleep(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Retry-After", "30")
		http.Error(writer, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(time.Second)
	client.MaxAttempts = 2
	var response UsageResponse
	started := time.Now()
	_, err := client.fetchJSON(context.Background(), server.URL, &AuthConfig{AccessToken: "token", AccountID: "account"}, &response)
	if time.Since(started) > 3*time.Second {
		t.Fatal("client slept for a long Retry-After instead of returning")
	}
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Kind != "rate_limited" || apiError.RetryAfter != 30*time.Second {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestCrossHostRedirectIsRejected(t *testing.T) {
	var destinationCalled atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		destinationCalled.Store(true)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"plan_type":"pro"}`))
	}))
	defer destination.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusFound)
	}))
	defer origin.Close()

	client := NewClient(time.Second)
	client.MaxAttempts = 1
	var response UsageResponse
	_, err := client.fetchJSON(context.Background(), origin.URL, &AuthConfig{AccessToken: "must-not-leak", AccountID: "account"}, &response)
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Kind != "unsafe_redirect" {
		t.Fatalf("unexpected redirect error: %#v", err)
	}
	if destinationCalled.Load() {
		t.Fatal("redirect destination was called")
	}
	if strings.Contains(err.Error(), "must-not-leak") {
		t.Fatal("error leaked token")
	}
}

func TestUnauthorizedDoesNotLeakBodyOrToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"access_token":"secret-from-proxy"}`))
	}))
	defer server.Close()

	client := NewClient(time.Second)
	client.MaxAttempts = 1
	var response UsageResponse
	_, err := client.fetchJSON(context.Background(), server.URL, &AuthConfig{AccessToken: "local-secret", AccountID: "account"}, &response)
	if err == nil {
		t.Fatal("expected 401 error")
	}
	text := err.Error()
	if !strings.Contains(text, "codex login") || strings.Contains(text, "secret") {
		t.Fatalf("unsafe or non-actionable error: %q", text)
	}
}
