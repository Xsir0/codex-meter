package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Report is the normalized, credential-safe view used by the CLI and --json.
// Status is retained only for exit-code handling and is not rendered as a card.
type Report struct {
	GeneratedAt  time.Time           `json:"generated_at"`
	Status       string              `json:"-"`
	Auth         AuthReport          `json:"auth"`
	Usage        *UsageReport        `json:"usage,omitempty"`
	ResetCredits *ResetCreditsReport `json:"reset_credits,omitempty"`
	Warnings     []string            `json:"warnings,omitempty"`
	Errors       []ReportError       `json:"errors,omitempty"`
}

type AuthReport struct {
	Path             string     `json:"path"`
	Mode             string     `json:"mode,omitempty"`
	AccountID        string     `json:"account_id"`
	AccountIDMasked  bool       `json:"account_id_masked"`
	Email            string     `json:"email,omitempty"`
	EmailMasked      bool       `json:"email_masked,omitempty"`
	LastRefresh      string     `json:"last_refresh,omitempty"`
	TokenIssuedAt    *time.Time `json:"token_issued_at,omitempty"`
	TokenExpiresAt   *time.Time `json:"token_expires_at,omitempty"`
	TokenExpiresIn   string     `json:"token_expires_in,omitempty"`
	TokenPlanType    string     `json:"token_plan_type,omitempty"`
	CredentialStatus string     `json:"credential_status"`
}

type UsageReport struct {
	PlanType              string                  `json:"plan_type,omitempty"`
	Allowed               *bool                   `json:"allowed,omitempty"`
	LimitReached          *bool                   `json:"limit_reached,omitempty"`
	PrimaryWindow         *WindowReport           `json:"primary_window,omitempty"`
	SecondaryWindow       *WindowReport           `json:"secondary_window,omitempty"`
	Spark                 *FeatureRateLimitReport `json:"spark,omitempty"`
	ResetCreditsFromUsage *int64                  `json:"reset_credits_available_count,omitempty"`
}

// FeatureRateLimitReport keeps only normalized quota fields. Upstream extension
// fields are omitted from human and normalized JSON output; use --raw to inspect them.
type FeatureRateLimitReport struct {
	Label           string        `json:"label"`
	Allowed         *bool         `json:"allowed,omitempty"`
	LimitReached    *bool         `json:"limit_reached,omitempty"`
	PrimaryWindow   *WindowReport `json:"primary_window,omitempty"`
	SecondaryWindow *WindowReport `json:"secondary_window,omitempty"`
}

type WindowReport struct {
	Label              string     `json:"label"`
	UsedPercent        *int       `json:"used_percent,omitempty"`
	RemainingPercent   *int       `json:"remaining_percent,omitempty"`
	LimitWindowSeconds *int64     `json:"limit_window_seconds,omitempty"`
	ResetAfterSeconds  *int64     `json:"reset_after_seconds,omitempty"`
	ResetAt            *time.Time `json:"reset_at,omitempty"`
	ResetIn            string     `json:"reset_in,omitempty"`
}

type ResetCreditsReport struct {
	AvailableCount         int                 `json:"available_count"`
	AvailableCountProvided bool                `json:"available_count_provided"`
	Credits                []ResetCreditReport `json:"credits"`
	DroppedMalformed       int                 `json:"dropped_malformed,omitempty"`
}

type ResetCreditReport struct {
	// ID is retained internally for deterministic sorting, but it is omitted
	// from the human dashboard and normalized JSON. Use --raw when the upstream
	// identifier is needed for troubleshooting.
	ID              string     `json:"-"`
	ResetType       string     `json:"reset_type"`
	Status          string     `json:"status"`
	StatusLabel     string     `json:"status_label"`
	Urgency         string     `json:"urgency"`
	UrgencyLabel    string     `json:"urgency_label"`
	GrantedAt       string     `json:"granted_at,omitempty"`
	GrantedTime     *time.Time `json:"granted_time,omitempty"`
	ExpiresAt       string     `json:"expires_at,omitempty"`
	ExpiresTime     *time.Time `json:"expires_time,omitempty"`
	ExpiresIn       string     `json:"expires_in,omitempty"`
	RedeemStartedAt string     `json:"redeem_started_at,omitempty"`
	RedeemStarted   *time.Time `json:"redeem_started_time,omitempty"`
	RedeemedAt      string     `json:"redeemed_at,omitempty"`
	RedeemedTime    *time.Time `json:"redeemed_time,omitempty"`
	Title           string     `json:"title,omitempty"`
	Description     string     `json:"description,omitempty"`
}

type ReportError struct {
	Section    string `json:"section"`
	Message    string `json:"message"`
	Kind       string `json:"kind,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	RetryAfter string `json:"retry_after,omitempty"`
}

func BuildReport(auth *AuthConfig, snapshot Snapshot, now time.Time, showSensitiveAccount bool) Report {
	report := Report{GeneratedAt: snapshot.FetchedAt, Status: "ok"}
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = now
	}
	if snapshot.Failed() {
		report.Status = "failed"
	} else if snapshot.Partial() {
		report.Status = "partial"
	}

	if auth != nil {
		report.Auth = buildAuthReport(auth, now, showSensitiveAccount)
		report.Warnings = append(report.Warnings, auth.Warnings...)
	}
	if snapshot.Usage != nil {
		report.Usage = buildUsageReport(snapshot.Usage, now)
	}
	if snapshot.ResetCredits != nil {
		report.ResetCredits = buildResetCreditsReport(snapshot.ResetCredits, now)
		if snapshot.ResetCredits.DroppedCredits > 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"Skipped %d malformed reset-credit record(s); valid records are still shown.",
				snapshot.ResetCredits.DroppedCredits,
			))
		}
	}
	if snapshot.UsageError != nil {
		report.Errors = append(report.Errors, reportError("usage", snapshot.UsageError))
	}
	if snapshot.ResetCreditsError != nil {
		report.Errors = append(report.Errors, reportError("reset_credits", snapshot.ResetCreditsError))
	}
	return report
}

func buildAuthReport(auth *AuthConfig, now time.Time, showSensitive bool) AuthReport {
	account := auth.AccountID
	email := auth.Token.Email
	masked := !showSensitive
	if masked {
		account = MaskIdentifier(account)
		email = MaskEmail(email)
	}
	report := AuthReport{
		Path:             auth.Path,
		Mode:             auth.AuthMode,
		AccountID:        account,
		AccountIDMasked:  masked,
		Email:            email,
		EmailMasked:      masked && email != "",
		LastRefresh:      auth.LastRefresh,
		TokenPlanType:    auth.Token.PlanType,
		CredentialStatus: "valid-looking",
	}
	if !auth.Token.IssuedAt.IsZero() {
		issued := auth.Token.IssuedAt
		report.TokenIssuedAt = &issued
	}
	if !auth.Token.ExpiresAt.IsZero() {
		expires := auth.Token.ExpiresAt
		report.TokenExpiresAt = &expires
		report.TokenExpiresIn = RelativeDuration(expires.Sub(now))
		if !expires.After(now) {
			report.CredentialStatus = "expired"
		} else if expires.Sub(now) <= 15*time.Minute {
			report.CredentialStatus = "expiring-soon"
		}
	} else {
		report.CredentialStatus = "unknown-expiry"
	}
	return report
}

func buildUsageReport(usage *UsageResponse, now time.Time) *UsageReport {
	report := &UsageReport{PlanType: usage.PlanType}
	if usage.RateLimit != nil {
		report.Allowed = usage.RateLimit.Allowed.Ptr()
		report.LimitReached = usage.RateLimit.LimitReached.Ptr()
		if usage.RateLimit.PrimaryWindow != nil {
			report.PrimaryWindow = buildWindowReport("primary", usage.RateLimit.PrimaryWindow, now)
		}
		if usage.RateLimit.SecondaryWindow != nil {
			report.SecondaryWindow = buildWindowReport("secondary", usage.RateLimit.SecondaryWindow, now)
		}
	}
	if spark := selectSparkRateLimit(usage.AdditionalRateLimits); spark != nil && spark.RateLimit != nil {
		report.Spark = buildFeatureRateLimitReport("Spark", spark.RateLimit, now)
	}
	if usage.RateLimitResetCredits != nil {
		report.ResetCreditsFromUsage = usage.RateLimitResetCredits.AvailableCount.Ptr()
	}
	return report
}

func selectSparkRateLimit(limits []AdditionalRateLimit) *AdditionalRateLimit {
	var fallback *AdditionalRateLimit
	for index := range limits {
		limit := &limits[index]
		if strings.EqualFold(strings.TrimSpace(limit.MeteredFeature), "codex_bengalfox") {
			return limit
		}
		if fallback == nil && limit.IsSpark() {
			fallback = limit
		}
	}
	return fallback
}

func buildFeatureRateLimitReport(label string, limit *RateLimit, now time.Time) *FeatureRateLimitReport {
	report := &FeatureRateLimitReport{
		Label:        label,
		Allowed:      limit.Allowed.Ptr(),
		LimitReached: limit.LimitReached.Ptr(),
	}
	if limit.PrimaryWindow != nil {
		report.PrimaryWindow = buildWindowReport("primary", limit.PrimaryWindow, now)
	}
	if limit.SecondaryWindow != nil {
		report.SecondaryWindow = buildWindowReport("secondary", limit.SecondaryWindow, now)
	}
	return report
}

func buildWindowReport(role string, window *UsageLimitWindow, now time.Time) *WindowReport {
	report := &WindowReport{
		Label:              WindowLabel(role, window.LimitWindowSeconds),
		LimitWindowSeconds: window.LimitWindowSeconds.Ptr(),
		ResetAfterSeconds:  window.ResetAfterSeconds.Ptr(),
	}
	if used, valid := window.UsedPercentClamped(); valid {
		report.UsedPercent = intPointer(used)
	}
	if remaining, valid := window.RemainingPercent(); valid {
		report.RemainingPercent = intPointer(remaining)
	}
	if reset, valid := window.ResetDate(now); valid {
		report.ResetAt = &reset
		report.ResetIn = RelativeDuration(reset.Sub(now))
	}
	return report
}

func buildResetCreditsReport(credits *ResetCreditsResponse, now time.Time) *ResetCreditsReport {
	report := &ResetCreditsReport{
		AvailableCount:         credits.AvailableCount,
		AvailableCountProvided: credits.AvailableCountProvided,
		DroppedMalformed:       credits.DroppedCredits,
	}
	for _, credit := range credits.Credits {
		report.Credits = append(report.Credits, buildResetCreditReport(credit, now))
	}
	// Put usable, soon-to-expire records first for quick scanning.
	sort.SliceStable(report.Credits, func(i, j int) bool {
		left, right := report.Credits[i], report.Credits[j]
		leftRank, rightRank := urgencyRank(left.Urgency), urgencyRank(right.Urgency)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.ExpiresTime != nil && right.ExpiresTime != nil {
			return left.ExpiresTime.Before(*right.ExpiresTime)
		}
		return left.ID < right.ID
	})
	return report
}

func buildResetCreditReport(credit ResetCredit, now time.Time) ResetCreditReport {
	urgency, urgencyLabel := CreditUrgency(credit, now)
	report := ResetCreditReport{
		ID:              credit.ID,
		ResetType:       credit.ResetType,
		Status:          credit.Status,
		StatusLabel:     CreditStatusLabel(credit.Status),
		Urgency:         urgency,
		UrgencyLabel:    urgencyLabel,
		GrantedAt:       credit.GrantedAt,
		ExpiresAt:       credit.ExpiresAt,
		RedeemStartedAt: credit.RedeemStartedAt,
		RedeemedAt:      credit.RedeemedAt,
		Title:           credit.Title,
		Description:     credit.Description,
	}
	if parsed, valid := ParseAPITime(credit.GrantedAt); valid {
		report.GrantedTime = &parsed
	}
	if parsed, valid := ParseAPITime(credit.ExpiresAt); valid {
		report.ExpiresTime = &parsed
		report.ExpiresIn = RelativeDuration(parsed.Sub(now))
	}
	if parsed, valid := ParseAPITime(credit.RedeemStartedAt); valid {
		report.RedeemStarted = &parsed
	}
	if parsed, valid := ParseAPITime(credit.RedeemedAt); valid {
		report.RedeemedTime = &parsed
	}
	return report
}

func reportError(section string, err error) ReportError {
	report := ReportError{Section: section, Message: err.Error()}
	var apiError *APIError
	if errorsAs(err, &apiError) {
		report.Kind = apiError.Kind
		report.StatusCode = apiError.StatusCode
		if apiError.RetryAfter > 0 {
			report.RetryAfter = apiError.RetryAfter.String()
		}
	}
	return report
}

// Keep APIError internals out of the UI package.
func errorsAs(err error, target any) bool {
	return asError(err, target)
}

func CreditUrgency(credit ResetCredit, now time.Time) (code, label string) {
	if !credit.IsAvailable() {
		return "used", "Unavailable"
	}
	expires, valid := ParseAPITime(credit.ExpiresAt)
	if !valid {
		return "unknown", "Expiry unknown"
	}
	remaining := expires.Sub(now)
	switch {
	case remaining <= 0:
		return "expired", "Expired"
	case remaining <= 24*time.Hour:
		return "ends_today", "Expires today"
	case remaining <= 3*24*time.Hour:
		return "expires_soon", "Expires soon"
	case remaining <= 7*24*time.Hour:
		return "this_week", "Expires this week"
	default:
		return "available", "Available"
	}
}

func CreditStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "available":
		return "Available"
	case "redeemed", "used":
		return "Used"
	case "expired":
		return "Expired"
	case "redeeming", "in_progress", "processing":
		return "In progress"
	case "revoked", "cancelled", "canceled":
		return "Revoked"
	case "":
		return "Unknown"
	default:
		return humanizeIdentifier(status)
	}
}

func PlanLabel(plan string) string {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "free", "guest":
		return "Free"
	case "plus":
		return "Plus"
	case "pro":
		return "Pro"
	case "prolite", "pro_lite", "pro-lite":
		return "Pro Lite"
	case "team":
		return "Team"
	case "business":
		return "Business"
	case "enterprise":
		return "Enterprise"
	case "education", "edu":
		return "Education"
	case "free_workspace":
		return "Free Workspace"
	case "":
		return "Unknown"
	default:
		return humanizeIdentifier(plan)
	}
}

func AuthModeLabel(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "chatgpt", "oauth", "chatgpt_oauth":
		return "ChatGPT OAuth"
	case "api_key", "apikey", "api-key":
		return "API key"
	case "":
		return "ChatGPT OAuth"
	default:
		return humanizeIdentifier(mode)
	}
}

func ResetTypeLabel(resetType string) string {
	switch strings.ToLower(strings.TrimSpace(resetType)) {
	case "rate_limit", "rate-limit", "rate_limit_reset":
		return "Rate-limit reset"
	case "manual", "manual_reset":
		return "Manual reset"
	case "weekly", "weekly_reset":
		return "Weekly reset"
	case "":
		return "Unknown"
	default:
		return humanizeIdentifier(resetType)
	}
}

func WindowLabel(role string, seconds OptionalInt) string {
	if seconds.Valid {
		duration := time.Duration(seconds.Value) * time.Second
		switch {
		case duration >= 4*time.Hour && duration <= 6*time.Hour:
			return "5-hour quota"
		case duration >= 6*24*time.Hour && duration <= 8*24*time.Hour:
			return "Weekly quota"
		case duration >= 23*time.Hour && duration <= 25*time.Hour:
			return "Daily quota"
		default:
			return "Quota window (" + CompactDuration(duration) + ")"
		}
	}
	if role == "primary" {
		return "5-hour quota"
	}
	if role == "secondary" {
		return "Weekly quota"
	}
	return "Quota window"
}

func HumanDuration(duration time.Duration) string {
	if duration < 0 {
		duration = -duration
	}
	if duration < time.Minute {
		seconds := int(duration.Round(time.Second) / time.Second)
		if seconds <= 1 {
			return "less than 1 second"
		}
		return plural(seconds, "second")
	}
	days := int(duration / (24 * time.Hour))
	duration %= 24 * time.Hour
	hours := int(duration / time.Hour)
	duration %= time.Hour
	minutes := int(duration / time.Minute)
	parts := make([]string, 0, 2)
	if days > 0 {
		parts = append(parts, plural(days, "day"))
	}
	if hours > 0 && len(parts) < 2 {
		parts = append(parts, plural(hours, "hour"))
	}
	if minutes > 0 && len(parts) < 2 {
		parts = append(parts, plural(minutes, "minute"))
	}
	if len(parts) == 0 {
		return "less than 1 minute"
	}
	return strings.Join(parts, " ")
}

func RelativeDuration(duration time.Duration) string {
	if duration < 0 {
		return HumanDuration(duration) + " ago"
	}
	return "in " + HumanDuration(duration)
}

func CompactDuration(duration time.Duration) string {
	if duration < 0 {
		duration = -duration
	}
	if duration%time.Hour == 0 {
		hours := int(duration / time.Hour)
		if hours%24 == 0 {
			return fmt.Sprintf("%dd", hours/24)
		}
		return fmt.Sprintf("%dh", hours)
	}
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	}
	return duration.Round(time.Second).String()
}

func intPointer(value int) *int { return &value }

func urgencyRank(urgency string) int {
	switch urgency {
	case "ends_today":
		return 0
	case "expires_soon":
		return 1
	case "this_week":
		return 2
	case "available":
		return 3
	case "unknown":
		return 4
	case "expired":
		return 5
	default:
		return 6
	}
}

func humanizeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	value = strings.NewReplacer("_", " ", "-", " ").Replace(value)
	words := strings.Fields(value)
	for index, word := range words {
		lower := strings.ToLower(word)
		switch lower {
		case "api", "id", "oauth", "gpt":
			words[index] = strings.ToUpper(lower)
		default:
			words[index] = strings.ToUpper(lower[:1]) + lower[1:]
		}
	}
	return strings.Join(words, " ")
}

func plural(value int, unit string) string {
	if value == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", value, unit)
}
