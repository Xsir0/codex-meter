package ui

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Xsir0/codex-meter/internal/core"
)

func TestRenderNoColorEnglishFixedWidthAndNoRemovedSections(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	remainingPrimary, usedPrimary := 75, 25
	remainingWeekly, usedWeekly := 60, 40
	remainingSpark, usedSpark := 90, 10
	resetPrimary, resetWeekly := now.Add(time.Hour), now.Add(4*24*time.Hour)
	resetSpark := now.Add(2 * time.Hour)
	expires := now.Add(6 * 24 * time.Hour)
	report := core.Report{
		GeneratedAt: now,
		Status:      "ok",
		Auth:        core.AuthReport{Path: "/home/test/.codex/auth.json", AccountID: "acct…1234", CredentialStatus: "valid-looking", TokenPlanType: "pro"},
		Usage: &core.UsageReport{
			PlanType:        "pro",
			PrimaryWindow:   &core.WindowReport{Label: "5-hour quota", RemainingPercent: &remainingPrimary, UsedPercent: &usedPrimary, ResetAt: &resetPrimary, ResetIn: "in 1 hour"},
			SecondaryWindow: &core.WindowReport{Label: "Weekly quota", RemainingPercent: &remainingWeekly, UsedPercent: &usedWeekly, ResetAt: &resetWeekly, ResetIn: "in 4 days"},
			Spark: &core.FeatureRateLimitReport{
				Label:         "Spark",
				PrimaryWindow: &core.WindowReport{Label: "5-hour quota", RemainingPercent: &remainingSpark, UsedPercent: &usedSpark, ResetAt: &resetSpark, ResetIn: "in 2 hours"},
			},
		},
		ResetCredits: &core.ResetCreditsReport{AvailableCount: 1, Credits: []core.ResetCreditReport{{
			ID: "credit-1", Status: "available", StatusLabel: "Available", Urgency: "this_week", UrgencyLabel: "Expires this week", ExpiresTime: &expires, ExpiresIn: "in 6 days", Title: "Reset Credit",
		}}},
	}
	var output bytes.Buffer
	if err := Render(&output, report, Options{Color: false, Unicode: true, Width: 76}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{"CODEX METER", "USAGE", "CODEX USAGE", "SPARK USAGE", "RESET CREDITS", "5-hour quota", "Weekly quota"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q:\n%s", expected, text)
		}
	}
	for _, forbidden := range []string{"RECOMMENDATION", "DIAGNOSTICS", "EXTENSIONS", "hidden_extension", "credit-1", " ID "} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("output unexpectedly contains %q:\n%s", forbidden, text)
		}
	}
	if strings.Count(text, "╭─ USAGE ") != 1 {
		t.Fatalf("Codex and Spark were not rendered in exactly one usage card:\n%s", text)
	}
	if !strings.Contains(text, "├") {
		t.Fatalf("combined usage card is missing its internal divider:\n%s", text)
	}
	if strings.Contains(text, "╭─ STATUS ") || strings.Contains(text, "+-- STATUS ") {
		t.Fatalf("output contains the removed status card:\n%s", text)
	}
	if regexp.MustCompile(`[\p{Han}]`).MatchString(text) {
		t.Fatalf("output contains non-English Han characters:\n%s", text)
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatal("no-color output contains ANSI escapes")
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if strings.HasPrefix(line, "╭") || strings.HasPrefix(line, "│") || strings.HasPrefix(line, "╰") {
			if width := visibleWidth(line); width != 76 {
				t.Fatalf("line width = %d, want 76: %q", width, line)
			}
		}
	}
}

func TestRenderSparkMissingMessage(t *testing.T) {
	report := core.Report{
		GeneratedAt: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		Status:      "ok",
		Auth:        core.AuthReport{Path: "/tmp/auth.json", AccountID: "acct…1234", CredentialStatus: "valid-looking"},
		Usage:       &core.UsageReport{PlanType: "pro"},
	}
	var output bytes.Buffer
	if err := Render(&output, report, Options{Color: false, Unicode: true, Width: 76}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "No Spark quota was returned for this account.") {
		t.Fatalf("missing Spark fallback message:\n%s", output.String())
	}
}

func TestWrapANSIPrefersSpaces(t *testing.T) {
	lines := wrapANSI("Run codex login and try again", 16)
	joined := strings.Join(lines, "|")
	if strings.Contains(joined, "log|in") || strings.Contains(joined, "aga|in") {
		t.Fatalf("ASCII word was split: %q", joined)
	}
}
