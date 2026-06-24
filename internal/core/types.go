package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// OptionalInt accepts JSON integers, floating-point values, and numeric strings.
// Unsupported values are treated as absent so an added/changed server field does
// not make the whole dashboard unusable.
type OptionalInt struct {
	Value int64
	Valid bool
}

func (v *OptionalInt) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		return nil
	}
	var n json.Number
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&n); err == nil {
		if i, err := n.Int64(); err == nil {
			v.Value, v.Valid = i, true
			return nil
		}
		if f, err := strconv.ParseFloat(n.String(), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			v.Value, v.Valid = int64(math.Round(f)), true
			return nil
		}
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		s = strings.TrimSpace(s)
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			v.Value, v.Valid = i, true
			return nil
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			v.Value, v.Valid = int64(math.Round(f)), true
		}
	}
	return nil
}

func (v OptionalInt) Int() (int, bool) {
	if !v.Valid {
		return 0, false
	}
	return int(v.Value), true
}

func (v OptionalInt) Ptr() *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Value
	return &n
}

type OptionalFloat struct {
	Value float64
	Valid bool
}

func (v *OptionalFloat) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		return nil
	}
	var raw any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil
	}
	switch x := raw.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(x.String(), 64)
		if err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			v.Value, v.Valid = f, true
		}
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			v.Value, v.Valid = f, true
		}
	}
	return nil
}

type OptionalBool struct {
	Value bool
	Valid bool
}

func (v *OptionalBool) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		return nil
	}
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		v.Value, v.Valid = b, true
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "1", "yes":
			v.Value, v.Valid = true, true
		case "false", "0", "no":
			v.Value, v.Valid = false, true
		}
		return nil
	}
	var n json.Number
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&n); err == nil {
		if i, err := n.Int64(); err == nil && (i == 0 || i == 1) {
			v.Value, v.Valid = i == 1, true
		}
	}
	return nil
}

func (v OptionalBool) Ptr() *bool {
	if !v.Valid {
		return nil
	}
	b := v.Value
	return &b
}

type UsageResponse struct {
	PlanType              string                `json:"plan_type,omitempty"`
	RateLimit             *RateLimit            `json:"rate_limit,omitempty"`
	AdditionalRateLimits  []AdditionalRateLimit `json:"additional_rate_limits,omitempty"`
	RateLimitResetCredits *ResetCreditCount     `json:"rate_limit_reset_credits,omitempty"`
	Extra                 map[string]any        `json:"-"`
}

// Decode optional sections independently so one malformed Spark item cannot
// hide the normal Codex quota.
func (u *UsageResponse) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}

	u.PlanType = flexibleStringOr(object["plan_type"], "")
	if raw, ok := object["rate_limit"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var limit RateLimit
		if err := json.Unmarshal(raw, &limit); err != nil {
			return fmt.Errorf("invalid primary quota field: %w", err)
		}
		u.RateLimit = &limit
	}
	if raw, ok := object["additional_rate_limits"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err == nil {
			for _, item := range items {
				var limit AdditionalRateLimit
				if err := json.Unmarshal(item, &limit); err == nil && limit.RateLimit != nil {
					u.AdditionalRateLimits = append(u.AdditionalRateLimits, limit)
				}
			}
		}
	}
	if raw, ok := object["rate_limit_reset_credits"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var count ResetCreditCount
		if err := json.Unmarshal(raw, &count); err == nil {
			u.RateLimitResetCredits = &count
		}
	}

	extra, err := decodeExtraFields(data, map[string]struct{}{
		"plan_type": {}, "rate_limit": {}, "additional_rate_limits": {}, "rate_limit_reset_credits": {},
	})
	if err != nil {
		return err
	}
	u.Extra = extra
	return nil
}

// AdditionalRateLimit represents a feature-specific quota. Spark is currently
// returned in this collection rather than in the main rate_limit object.
type AdditionalRateLimit struct {
	LimitName      string         `json:"limit_name,omitempty"`
	MeteredFeature string         `json:"metered_feature,omitempty"`
	RateLimit      *RateLimit     `json:"rate_limit,omitempty"`
	Extra          map[string]any `json:"-"`
}

func (r *AdditionalRateLimit) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	r.LimitName = flexibleStringOr(object["limit_name"], "")
	r.MeteredFeature = flexibleStringOr(object["metered_feature"], "")
	if raw, ok := object["rate_limit"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var limit RateLimit
		if err := json.Unmarshal(raw, &limit); err != nil {
			return err
		}
		r.RateLimit = &limit
	}
	extra, err := decodeExtraFields(data, map[string]struct{}{
		"limit_name": {}, "metered_feature": {}, "rate_limit": {},
	})
	if err != nil {
		return err
	}
	r.Extra = extra
	return nil
}

func (r AdditionalRateLimit) IsSpark() bool {
	name := strings.ToLower(strings.TrimSpace(r.LimitName))
	feature := strings.ToLower(strings.TrimSpace(r.MeteredFeature))
	return feature == "codex_bengalfox" || strings.Contains(name, "spark") || strings.Contains(feature, "spark")
}

type RateLimit struct {
	Allowed         OptionalBool      `json:"allowed"`
	LimitReached    OptionalBool      `json:"limit_reached"`
	PrimaryWindow   *UsageLimitWindow `json:"primary_window,omitempty"`
	SecondaryWindow *UsageLimitWindow `json:"secondary_window,omitempty"`
	Extra           map[string]any    `json:"-"`
}

func (r *RateLimit) UnmarshalJSON(data []byte) error {
	type alias RateLimit
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = RateLimit(decoded)
	extra, err := decodeExtraFields(data, map[string]struct{}{
		"allowed": {}, "limit_reached": {}, "primary_window": {}, "secondary_window": {},
	})
	if err != nil {
		return err
	}
	r.Extra = extra
	return nil
}

type UsageLimitWindow struct {
	UsedPercent        OptionalInt    `json:"used_percent"`
	LimitWindowSeconds OptionalInt    `json:"limit_window_seconds"`
	ResetAfterSeconds  OptionalInt    `json:"reset_after_seconds"`
	ResetAt            OptionalFloat  `json:"reset_at"`
	Extra              map[string]any `json:"-"`
}

func (w *UsageLimitWindow) UnmarshalJSON(data []byte) error {
	type alias UsageLimitWindow
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*w = UsageLimitWindow(decoded)
	extra, err := decodeExtraFields(data, map[string]struct{}{
		"used_percent": {}, "limit_window_seconds": {}, "reset_after_seconds": {}, "reset_at": {},
	})
	if err != nil {
		return err
	}
	w.Extra = extra
	return nil
}

func (w UsageLimitWindow) RemainingPercent() (int, bool) {
	used, ok := w.UsedPercent.Int()
	if !ok {
		return 0, false
	}
	return clamp(100-used, 0, 100), true
}

func (w UsageLimitWindow) UsedPercentClamped() (int, bool) {
	used, ok := w.UsedPercent.Int()
	if !ok {
		return 0, false
	}
	return clamp(used, 0, 100), true
}

func (w UsageLimitWindow) ResetDate(now time.Time) (time.Time, bool) {
	if w.ResetAt.Valid {
		seconds := w.ResetAt.Value
		if seconds > 10_000_000_000 {
			seconds /= 1000
		}
		sec, frac := math.Modf(seconds)
		return time.Unix(int64(sec), int64(frac*float64(time.Second))), true
	}
	if seconds, ok := w.ResetAfterSeconds.Int(); ok {
		return now.Add(time.Duration(max(seconds, 0)) * time.Second), true
	}
	return time.Time{}, false
}

type ResetCreditCount struct {
	AvailableCount OptionalInt    `json:"available_count"`
	Extra          map[string]any `json:"-"`
}

func (r *ResetCreditCount) UnmarshalJSON(data []byte) error {
	type alias ResetCreditCount
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = ResetCreditCount(decoded)
	extra, err := decodeExtraFields(data, map[string]struct{}{"available_count": {}})
	if err != nil {
		return err
	}
	r.Extra = extra
	return nil
}

type ResetCreditsResponse struct {
	Credits                []ResetCredit  `json:"credits"`
	AvailableCount         int            `json:"available_count"`
	AvailableCountProvided bool           `json:"-"`
	DroppedCredits         int            `json:"-"`
	Extra                  map[string]any `json:"-"`
}

func (r *ResetCreditsResponse) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}

	if raw, ok := object["credits"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("credits is not an array: %w", err)
		}
		for _, item := range items {
			var credit ResetCredit
			if err := json.Unmarshal(item, &credit); err != nil {
				r.DroppedCredits++
				continue
			}
			r.Credits = append(r.Credits, credit)
		}
	}

	if raw, ok := object["available_count"]; ok {
		var count OptionalInt
		_ = json.Unmarshal(raw, &count)
		if n, valid := count.Int(); valid {
			r.AvailableCount = max(n, 0)
			r.AvailableCountProvided = true
		}
	}
	if !r.AvailableCountProvided {
		for _, credit := range r.Credits {
			if credit.IsAvailable() {
				r.AvailableCount++
			}
		}
	}

	extra, err := decodeExtraFields(data, map[string]struct{}{
		"credits": {}, "available_count": {},
	})
	if err != nil {
		return err
	}
	r.Extra = extra
	return nil
}

type ResetCredit struct {
	ID              string         `json:"id"`
	ResetType       string         `json:"reset_type"`
	Status          string         `json:"status"`
	GrantedAt       string         `json:"granted_at,omitempty"`
	ExpiresAt       string         `json:"expires_at,omitempty"`
	RedeemStartedAt string         `json:"redeem_started_at,omitempty"`
	RedeemedAt      string         `json:"redeemed_at,omitempty"`
	Title           string         `json:"title,omitempty"`
	Description     string         `json:"description,omitempty"`
	Extra           map[string]any `json:"-"`
}

func (c *ResetCredit) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	id, ok := flexibleString(object["id"])
	if !ok || strings.TrimSpace(id) == "" {
		return fmt.Errorf("reset-credit record is missing a valid ID")
	}
	c.ID = id
	c.ResetType = flexibleStringOr(object["reset_type"], "unknown")
	c.Status = flexibleStringOr(object["status"], "unknown")
	c.GrantedAt = flexibleStringOr(object["granted_at"], "")
	c.ExpiresAt = flexibleStringOr(object["expires_at"], "")
	c.RedeemStartedAt = flexibleStringOr(object["redeem_started_at"], "")
	c.RedeemedAt = flexibleStringOr(object["redeemed_at"], "")
	c.Title = flexibleStringOr(object["title"], "")
	c.Description = flexibleStringOr(object["description"], "")

	extra, err := decodeExtraFields(data, map[string]struct{}{
		"id": {}, "reset_type": {}, "status": {}, "granted_at": {}, "expires_at": {},
		"redeem_started_at": {}, "redeemed_at": {}, "title": {}, "description": {},
	})
	if err != nil {
		return err
	}
	c.Extra = extra
	return nil
}

func (c ResetCredit) IsAvailable() bool {
	return strings.EqualFold(strings.TrimSpace(c.Status), "available")
}

func flexibleString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var n json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&n); err == nil {
		return n.String(), true
	}
	return "", false
}

func flexibleStringOr(raw json.RawMessage, fallback string) string {
	if value, ok := flexibleString(raw); ok {
		return value
	}
	return fallback
}

func decodeExtraFields(data []byte, known map[string]struct{}) (map[string]any, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	extra := make(map[string]any)
	for key, raw := range object {
		if _, ok := known[key]; ok {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err == nil {
			extra[key] = value
		}
	}
	if len(extra) == 0 {
		return nil, nil
	}
	return extra, nil
}

func ParseAPITime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if numeric, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(numeric) && !math.IsInf(numeric, 0) {
		if numeric > 10_000_000_000 {
			numeric /= 1000
		}
		seconds, fraction := math.Modf(numeric)
		return time.Unix(int64(seconds), int64(fraction*float64(time.Second))), true
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
