package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCreditUrgencyBoundaries(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		status string
		expiry time.Duration
		want   string
	}{
		{"used", "redeemed", 10 * 24 * time.Hour, "used"},
		{"expired", "available", -time.Second, "expired"},
		{"today", "available", 12 * time.Hour, "ends_today"},
		{"soon", "available", 48 * time.Hour, "expires_soon"},
		{"week", "available", 6 * 24 * time.Hour, "this_week"},
		{"normal", "available", 8 * 24 * time.Hour, "available"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, _ := CreditUrgency(ResetCredit{Status: test.status, ExpiresAt: now.Add(test.expiry).Format(time.RFC3339)}, now)
			if code != test.want {
				t.Fatalf("urgency = %q, want %q", code, test.want)
			}
		})
	}
}

func TestBuildReportIncludesSparkQuota(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	used := OptionalInt{Value: 30, Valid: true}
	usage := &UsageResponse{
		PlanType: "pro",
		AdditionalRateLimits: []AdditionalRateLimit{
			{
				LimitName:      "GPT-5.3-Codex-Spark",
				MeteredFeature: "codex_bengalfox",
				RateLimit: &RateLimit{
					PrimaryWindow: &UsageLimitWindow{UsedPercent: used},
				},
			},
		},
	}
	report := BuildReport(
		&AuthConfig{Path: "/tmp/auth.json", AccountID: "acct-123", Token: TokenMetadata{PlanType: "pro"}},
		Snapshot{FetchedAt: now, Usage: usage, ResetCredits: &ResetCreditsResponse{}},
		now,
		false,
	)
	if report.Usage == nil || report.Usage.Spark == nil || report.Usage.Spark.PrimaryWindow == nil {
		t.Fatalf("Spark quota missing from report: %+v", report.Usage)
	}
	if remaining := report.Usage.Spark.PrimaryWindow.RemainingPercent; remaining == nil || *remaining != 70 {
		t.Fatalf("Spark remaining percent = %v, want 70", remaining)
	}
}

func TestNormalizedReportOmitsStatusAndExtensionFields(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	used := OptionalInt{Value: 20, Valid: true}
	report := BuildReport(
		&AuthConfig{Path: "/tmp/auth.json", AccountID: "acct-123456789", Token: TokenMetadata{PlanType: "pro"}},
		Snapshot{
			FetchedAt: now,
			Usage: &UsageResponse{
				RateLimit: &RateLimit{PrimaryWindow: &UsageLimitWindow{UsedPercent: used, Extra: map[string]any{"hidden_window_field": true}}, Extra: map[string]any{"hidden_rate_field": true}},
				AdditionalRateLimits: []AdditionalRateLimit{{
					LimitName: "Spark", MeteredFeature: "codex_bengalfox",
					RateLimit: &RateLimit{PrimaryWindow: &UsageLimitWindow{UsedPercent: used}},
					Extra:     map[string]any{"hidden_spark_field": true},
				}},
				Extra: map[string]any{"hidden_usage_field": true},
			},
			ResetCredits: &ResetCreditsResponse{Extra: map[string]any{"hidden_credit_field": true}},
		},
		now,
		false,
	)
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"\"status\"", "recommendation", "diagnostics", "extra", "hidden_"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("normalized JSON contains %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"spark"`) {
		t.Fatalf("normalized JSON does not contain Spark quota: %s", text)
	}
}

func TestNormalizedResetCreditOmitsUpstreamID(t *testing.T) {
	data, err := json.Marshal(ResetCreditReport{
		ID:          "rc_private_identifier",
		ResetType:   "rate_limit",
		Status:      "available",
		StatusLabel: "Available",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "rc_private_identifier") || strings.Contains(text, `"id"`) {
		t.Fatalf("normalized reset-credit JSON exposes the upstream ID: %s", text)
	}
}

func TestParseAPITimeUnixMilliseconds(t *testing.T) {
	parsed, ok := ParseAPITime("1782302400000")
	if !ok || parsed.Unix() != 1782302400 {
		t.Fatalf("parsed = %v, %v", parsed, ok)
	}
}

func TestBuildReportPartial(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	used := OptionalInt{Value: 20, Valid: true}
	report := BuildReport(
		&AuthConfig{Path: "/tmp/auth.json", AccountID: "acct-123456789", Token: TokenMetadata{PlanType: "pro"}},
		Snapshot{
			FetchedAt:         now,
			Usage:             &UsageResponse{RateLimit: &RateLimit{PrimaryWindow: &UsageLimitWindow{UsedPercent: used}}},
			ResetCreditsError: &APIError{Kind: "server", Message: "down"},
		},
		now,
		false,
	)
	if report.Status != "partial" || report.Usage.PrimaryWindow.RemainingPercent == nil || *report.Usage.PrimaryWindow.RemainingPercent != 80 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Auth.AccountID != "acct…6789" {
		t.Fatalf("account not masked: %q", report.Auth.AccountID)
	}
}
