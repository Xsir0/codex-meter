package core

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolveAuthPath(t *testing.T) {
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
	path, err := ResolveAuthPath("", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := path, filepath.Join(os.Getenv("CODEX_HOME"), "auth.json"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestLoadAuthExtractsNestedAccountAndMetadata(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	token := testJWT(t, map[string]any{
		"sub":   "user-123",
		"email": "person@example.com",
		"iat":   now.Add(-time.Hour).Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct_nested_12345678",
			"chatgpt_plan_type":  "pro",
		},
	})
	path := writeAuthFile(t, map[string]any{
		"auth_mode":    "chatgpt",
		"last_refresh": "2026-06-24T11:00:00Z",
		"tokens": map[string]any{
			"access_token": token,
			"account_id":   "fallback-account",
		},
	})

	auth, err := LoadAuth(path, now)
	if err != nil {
		t.Fatal(err)
	}
	if auth.AccountID != "acct_nested_12345678" {
		t.Fatalf("account id = %q", auth.AccountID)
	}
	if auth.Token.PlanType != "pro" || auth.Token.Email != "person@example.com" {
		t.Fatalf("unexpected token metadata: %+v", auth.Token)
	}
	if !auth.Token.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("expiry = %s", auth.Token.ExpiresAt)
	}
	if auth.AccessToken != token {
		t.Fatal("access token was not retained for HTTP requests")
	}
}

func TestLoadAuthFallsBackToTokensAccountID(t *testing.T) {
	token := testJWT(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	path := writeAuthFile(t, map[string]any{
		"tokens": map[string]any{
			"access_token": token,
			"account_id":   "fallback-account",
		},
	})
	auth, err := LoadAuth(path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if auth.AccountID != "fallback-account" {
		t.Fatalf("account id = %q", auth.AccountID)
	}
}

func TestLoadAuthAllowsMissingAccountID(t *testing.T) {
	token := testJWT(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	path := writeAuthFile(t, map[string]any{"tokens": map[string]any{"access_token": token}})
	auth, err := LoadAuth(path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if auth.AccountID != "" {
		t.Fatalf("unexpected account id: %q", auth.AccountID)
	}
	if !strings.Contains(strings.Join(auth.Warnings, "\n"), "ChatGPT-Account-Id header will not be sent") {
		t.Fatalf("missing warning: %v", auth.Warnings)
	}
}

func TestLoadAuthAPIKeyOnlyExplainsLoginMode(t *testing.T) {
	path := writeAuthFile(t, map[string]any{"OPENAI_API_KEY": "sk-redacted"})
	_, err := LoadAuth(path, time.Now())
	if err == nil || !strings.Contains(err.Error(), "codex login") {
		t.Fatalf("expected actionable API-key-only error, got %v", err)
	}
	if strings.Contains(err.Error(), "sk-redacted") {
		t.Fatal("error leaked API key")
	}
}

func TestLoadAuthWarnsAboutExpiredTokenAndPermissions(t *testing.T) {
	now := time.Now()
	token := testJWT(t, map[string]any{
		"exp": now.Add(-time.Minute).Unix(),
		"https://api.openai.com/auth.chatgpt_account_id": "acct-direct",
	})
	path := writeAuthFile(t, map[string]any{"tokens": map[string]any{"access_token": token}})
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	auth, err := LoadAuth(path, now)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(auth.Warnings, "\n")
	if !strings.Contains(joined, "has expired") {
		t.Fatalf("expected expiry warning, got %q", joined)
	}
	if runtime.GOOS != "windows" && !strings.Contains(joined, "chmod 600") {
		t.Fatalf("expected permission warning, got %q", joined)
	}
}

func TestMaskHelpers(t *testing.T) {
	if got := MaskIdentifier("account-123456789"); got != "acco…6789" {
		t.Fatalf("masked id = %q", got)
	}
	if got := MaskEmail("someone@example.com"); got != "s••••••@example.com" {
		t.Fatalf("masked email = %q", got)
	}
}

func writeAuthFile(t *testing.T, payload any) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "auth.json")
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
