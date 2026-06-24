package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Xsir0/codex-meter/internal/core"
)

type Options struct {
	Color       bool
	Unicode     bool
	Width       int
	ShowAccount bool
}

type palette struct{ enabled bool }

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
	ansiGray    = "\x1b[90m"
)

func (p palette) paint(code, text string) string {
	if !p.enabled || text == "" {
		return text
	}
	return code + text + ansiReset
}

func (p palette) bold(text string) string    { return p.paint(ansiBold, text) }
func (p palette) dim(text string) string     { return p.paint(ansiDim, text) }
func (p palette) red(text string) string     { return p.paint(ansiRed, text) }
func (p palette) green(text string) string   { return p.paint(ansiGreen, text) }
func (p palette) yellow(text string) string  { return p.paint(ansiYellow, text) }
func (p palette) blue(text string) string    { return p.paint(ansiBlue, text) }
func (p palette) cyan(text string) string    { return p.paint(ansiCyan, text) }
func (p palette) magenta(text string) string { return p.paint(ansiMagenta, text) }
func (p palette) gray(text string) string    { return p.paint(ansiGray, text) }

func Render(writer io.Writer, report core.Report, options Options) error {
	if options.Width <= 0 {
		options.Width = DetectWidth()
	}
	if options.Width < 58 {
		options.Width = 58
	}
	if options.Width > 132 {
		options.Width = 132
	}
	p := palette{enabled: options.Color}
	glyph := glyphsFor(options.Unicode)

	cards := [][]string{
		headerLines(report, p),
		authLines(report, p, glyph),
	}
	titles := []string{"CODEX METER", "ACCOUNT"}

	if report.Usage != nil {
		cards = append(cards, usageLines(report, p, glyph, options.Width))
		titles = append(titles, "USAGE")
	}
	if report.ResetCredits != nil {
		cards = append(cards, creditLines(report, p, glyph))
		titles = append(titles, "RESET CREDITS")
	}
	if len(report.Warnings) > 0 {
		cards = append(cards, warningLines(report, p, glyph))
		titles = append(titles, "WARNINGS")
	}
	if len(report.Errors) > 0 {
		cards = append(cards, errorLines(report, p, glyph))
		titles = append(titles, "ERRORS")
	}

	for index, lines := range cards {
		if index > 0 {
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
		if err := renderBox(writer, titles[index], lines, options.Width, p, glyph); err != nil {
			return err
		}
	}
	return nil
}

type glyphSet struct {
	topLeft, topRight, bottomLeft, bottomRight   string
	middleLeft, middleRight                      string
	horizontal, vertical, bullet, ok, warn, fail string
	barFull, barEmpty                            string
}

const boxDividerLine = "\x00codex-meter-box-divider\x00"

func glyphsFor(unicodeEnabled bool) glyphSet {
	if !unicodeEnabled {
		return glyphSet{
			topLeft: "+", topRight: "+", bottomLeft: "+", bottomRight: "+",
			middleLeft: "+", middleRight: "+",
			horizontal: "-", vertical: "|", bullet: "*", ok: "OK", warn: "!", fail: "X",
			barFull: "#", barEmpty: ".",
		}
	}
	return glyphSet{
		topLeft: "╭", topRight: "╮", bottomLeft: "╰", bottomRight: "╯",
		middleLeft: "├", middleRight: "┤",
		horizontal: "─", vertical: "│", bullet: "•", ok: "✓", warn: "!", fail: "×",
		barFull: "█", barEmpty: "░",
	}
}

func headerLines(report core.Report, p palette) []string {
	lines := []string{
		p.bold("Codex, Spark, and reset-credit usage at a glance"),
		p.dim("Updated: " + formatTime(report.GeneratedAt)),
		p.dim("Read-only: does not modify quotas, redeem credits, or upload credentials."),
	}
	if report.Status == "partial" {
		lines = append(lines, p.yellow("One endpoint failed; all available data is still shown."))
	} else if report.Status == "failed" {
		lines = append(lines, p.red("The quota request failed. See the error details below."))
	}
	return lines
}

func authLines(report core.Report, p palette, glyph glyphSet) []string {
	planType := report.Auth.TokenPlanType
	if report.Usage != nil && strings.TrimSpace(report.Usage.PlanType) != "" {
		planType = report.Usage.PlanType
	}
	lines := []string{
		p.bold("Account") + "  " + emptyDash(report.Auth.AccountID) + "    " + p.bold("Plan") + "  " + core.PlanLabel(planType),
		p.bold("Credentials") + "  " + shortenHome(report.Auth.Path),
	}
	if report.Auth.Email != "" {
		lines = append(lines, p.bold("Email")+"  "+report.Auth.Email)
	}
	credential := credentialLabel(report.Auth.CredentialStatus, p, glyph)
	line := p.bold("Authentication") + "  " + core.AuthModeLabel(report.Auth.Mode) + " · " + credential
	if report.Auth.TokenExpiresIn != "" {
		if report.Auth.CredentialStatus == "expired" {
			line += " · Token expired " + report.Auth.TokenExpiresIn
		} else {
			line += " · Token expires " + report.Auth.TokenExpiresIn
		}
	}
	lines = append(lines, line)
	if report.Auth.LastRefresh != "" {
		lines = append(lines, p.bold("Last refresh")+"  "+report.Auth.LastRefresh)
	}
	return lines
}

func credentialLabel(status string, p palette, glyph glyphSet) string {
	switch status {
	case "expired":
		return p.red(glyph.fail + " Expired")
	case "expiring-soon":
		return p.yellow(glyph.warn + " Expiring soon")
	case "unknown-expiry":
		return p.yellow(glyph.warn + " Expiry unknown")
	case "unavailable":
		return p.red(glyph.fail + " Unavailable")
	default:
		return p.green(glyph.ok + " Parsed")
	}
}

func codexUsageLines(report core.Report, p palette, glyph glyphSet, width int) []string {
	usage := report.Usage
	lines := make([]string, 0, 10)
	if usage.LimitReached != nil && *usage.LimitReached {
		lines = append(lines, p.red(glyph.warn+" The Codex quota is currently exhausted."), "")
	} else if usage.Allowed != nil && !*usage.Allowed {
		lines = append(lines, p.yellow(glyph.warn+" Codex access is currently unavailable."), "")
	}
	lines = append(lines, renderWindowPair(usage.PrimaryWindow, usage.SecondaryWindow, "No Codex quota windows were returned.", p, glyph, width)...)
	if usage.ResetCreditsFromUsage != nil && report.ResetCredits == nil {
		lines = append(lines, "", p.bold("Available reset credits")+fmt.Sprintf("  %d", *usage.ResetCreditsFromUsage))
	}
	return lines
}

func usageLines(report core.Report, p palette, glyph glyphSet, width int) []string {
	lines := []string{p.bold("CODEX USAGE")}
	lines = append(lines, codexUsageLines(report, p, glyph, width)...)
	lines = append(lines, boxDividerLine, p.bold("SPARK USAGE"))
	lines = append(lines, sparkUsageLines(report, p, glyph, width)...)
	return lines
}

func sparkUsageLines(report core.Report, p palette, glyph glyphSet, width int) []string {
	if report.Usage.Spark == nil {
		return []string{p.dim("No Spark quota was returned for this account.")}
	}
	spark := report.Usage.Spark
	lines := make([]string, 0, 10)
	if spark.LimitReached != nil && *spark.LimitReached {
		lines = append(lines, p.red(glyph.warn+" The Spark quota is currently exhausted."), "")
	} else if spark.Allowed != nil && !*spark.Allowed {
		lines = append(lines, p.yellow(glyph.warn+" Spark access is currently unavailable."), "")
	}
	lines = append(lines, renderWindowPair(spark.PrimaryWindow, spark.SecondaryWindow, "Spark was returned without quota windows.", p, glyph, width)...)
	return lines
}

func renderWindowPair(primary, secondary *core.WindowReport, emptyMessage string, p palette, glyph glyphSet, width int) []string {
	barWidth := 24
	if width < 76 {
		barWidth = 18
	}
	lines := make([]string, 0, 8)
	if primary != nil {
		lines = append(lines, renderWindow(primary, p, glyph, barWidth)...)
	}
	if primary != nil && secondary != nil {
		lines = append(lines, "")
	}
	if secondary != nil {
		lines = append(lines, renderWindow(secondary, p, glyph, barWidth)...)
	}
	if primary == nil && secondary == nil {
		lines = append(lines, p.yellow(glyph.warn+" "+emptyMessage))
	}
	return lines
}

func renderWindow(window *core.WindowReport, p palette, glyph glyphSet, barWidth int) []string {
	remainingText := "Remaining unknown"
	usedText := "Used unknown"
	bar := progressBar(nil, barWidth, p, glyph)
	if window.RemainingPercent != nil {
		remainingText = fmt.Sprintf("%d%% remaining", *window.RemainingPercent)
		bar = progressBar(window.RemainingPercent, barWidth, p, glyph)
	}
	if window.UsedPercent != nil {
		usedText = fmt.Sprintf("%d%% used", *window.UsedPercent)
	}
	lines := []string{
		p.bold(window.Label),
		"  " + bar + "  " + colorRemaining(remainingText, window.RemainingPercent, p),
	}
	details := []string{usedText}
	if window.ResetAt != nil {
		details = append(details, "Resets "+window.ResetIn+" ("+formatTime(*window.ResetAt)+")")
	} else if window.ResetAfterSeconds != nil {
		details = append(details, "Resets in about "+core.HumanDuration(timeDurationSeconds(*window.ResetAfterSeconds)))
	} else {
		details = append(details, "Reset time unknown")
	}
	if window.LimitWindowSeconds != nil {
		details = append(details, "Window "+core.CompactDuration(timeDurationSeconds(*window.LimitWindowSeconds)))
	}
	lines = append(lines, "  "+p.dim(strings.Join(details, " · ")))
	return lines
}

func progressBar(percent *int, width int, p palette, glyph glyphSet) string {
	if width < 4 {
		width = 4
	}
	value := 0
	if percent != nil {
		value = *percent
		if value < 0 {
			value = 0
		}
		if value > 100 {
			value = 100
		}
	}
	filled := (value*width + 50) / 100
	bar := strings.Repeat(glyph.barFull, filled) + strings.Repeat(glyph.barEmpty, width-filled)
	if percent == nil {
		return "[" + p.gray(bar) + "]"
	}
	switch {
	case value <= 12:
		bar = p.red(bar)
	case value <= 25:
		bar = p.yellow(bar)
	default:
		bar = p.green(bar)
	}
	return "[" + bar + "]"
}

func colorRemaining(text string, percent *int, p palette) string {
	if percent == nil {
		return p.gray(text)
	}
	switch {
	case *percent <= 12:
		return p.red(p.bold(text))
	case *percent <= 25:
		return p.yellow(p.bold(text))
	default:
		return p.green(p.bold(text))
	}
}

func creditLines(report core.Report, p palette, glyph glyphSet) []string {
	credits := report.ResetCredits
	lines := []string{
		p.bold("Available") + fmt.Sprintf("  %s", countCredit(credits.AvailableCount, p)) +
			"    " + p.bold("Records") + fmt.Sprintf("  %d", len(credits.Credits)),
	}
	if len(credits.Credits) == 0 {
		lines = append(lines, "", p.dim("No reset-credit records were returned."))
	}
	for index, credit := range credits.Credits {
		lines = append(lines, "")
		marker := urgencyMarker(credit.Urgency, p, glyph)
		title := strings.TrimSpace(credit.Title)
		if title == "" {
			title = "Reset credit"
		}
		lines = append(lines, fmt.Sprintf("%d. %s %s  %s", index+1, marker, p.bold(title), urgencyLabel(credit, p)))
		lines = append(lines,
			"   "+p.bold("Status")+" "+creditStatus(credit, p)+" · "+p.bold("Type")+" "+core.ResetTypeLabel(credit.ResetType),
		)
		if strings.TrimSpace(credit.Description) != "" {
			lines = append(lines, "   "+p.bold("Description")+" "+credit.Description)
		}
		if credit.GrantedTime != nil || credit.GrantedAt != "" {
			lines = append(lines, "   "+p.bold("Granted")+" "+displayAPITime(credit.GrantedTime, credit.GrantedAt))
		}
		if credit.ExpiresTime != nil || credit.ExpiresAt != "" {
			expires := displayAPITime(credit.ExpiresTime, credit.ExpiresAt)
			if credit.ExpiresIn != "" {
				expires += " (" + credit.ExpiresIn + ")"
			}
			lines = append(lines, "   "+p.bold("Expires")+" "+expires)
		}
		if credit.RedeemStarted != nil || credit.RedeemStartedAt != "" {
			lines = append(lines, "   "+p.bold("Redemption started")+" "+displayAPITime(credit.RedeemStarted, credit.RedeemStartedAt))
		}
		if credit.RedeemedTime != nil || credit.RedeemedAt != "" {
			lines = append(lines, "   "+p.bold("Redeemed")+" "+displayAPITime(credit.RedeemedTime, credit.RedeemedAt))
		}
	}
	if credits.DroppedMalformed > 0 {
		lines = append(lines, "", p.yellow(fmt.Sprintf("%s Skipped %d malformed record(s).", glyph.warn, credits.DroppedMalformed)))
	}
	return lines
}

func warningLines(report core.Report, p palette, glyph glyphSet) []string {
	lines := make([]string, 0, len(report.Warnings))
	for _, warning := range report.Warnings {
		lines = append(lines, p.yellow(glyph.warn)+" "+warning)
	}
	return lines
}

func errorLines(report core.Report, p palette, glyph glyphSet) []string {
	lines := make([]string, 0, len(report.Errors)*2)
	for _, item := range report.Errors {
		section := item.Section
		switch section {
		case "usage":
			section = "Usage"
		case "reset_credits":
			section = "Reset Credits"
		case "auth":
			section = "Authentication"
		default:
			section = "Request"
		}
		lines = append(lines, p.red(glyph.fail)+" "+p.bold(section)+"  "+item.Message)
		meta := make([]string, 0, 3)
		if item.Kind != "" {
			meta = append(meta, "Type "+item.Kind)
		}
		if item.StatusCode > 0 {
			meta = append(meta, fmt.Sprintf("HTTP %d", item.StatusCode))
		}
		if item.RetryAfter != "" {
			meta = append(meta, "Retry after "+item.RetryAfter)
		}
		if len(meta) > 0 {
			lines = append(lines, "  "+p.dim(strings.Join(meta, " · ")))
		}
	}
	return lines
}

func renderBox(writer io.Writer, title string, logicalLines []string, width int, p palette, glyph glyphSet) error {
	inner := width - 4
	if inner < 1 {
		inner = 1
	}
	titleText := " " + title + " "
	remaining := width - visibleWidth(glyph.topLeft+glyph.horizontal+titleText+glyph.topRight)
	if remaining < 0 {
		remaining = 0
	}
	top := glyph.topLeft + glyph.horizontal + titleText + strings.Repeat(glyph.horizontal, remaining) + glyph.topRight
	bottom := glyph.bottomLeft + strings.Repeat(glyph.horizontal, width-2) + glyph.bottomRight
	if _, err := fmt.Fprintln(writer, p.cyan(top)); err != nil {
		return err
	}
	for _, logical := range logicalLines {
		if logical == boxDividerLine {
			divider := glyph.middleLeft + strings.Repeat(glyph.horizontal, width-2) + glyph.middleRight
			if _, err := fmt.Fprintln(writer, p.cyan(divider)); err != nil {
				return err
			}
			continue
		}
		wrapped := wrapANSI(logical, inner)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for _, line := range wrapped {
			padding := inner - visibleWidth(line)
			if padding < 0 {
				padding = 0
			}
			if _, err := fmt.Fprintln(writer,
				p.cyan(glyph.vertical)+" "+line+strings.Repeat(" ", padding)+" "+p.cyan(glyph.vertical)); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(writer, p.cyan(bottom))
	return err
}

func wrapANSI(input string, width int) []string {
	if width <= 0 {
		return []string{input}
	}
	var result []string
	var line strings.Builder
	lineWidth := 0
	activeCodes := ""
	lastBreakByte := -1
	activeAtBreak := ""

	flush := func(force bool) {
		if line.Len() == 0 && !force {
			return
		}
		text := strings.TrimRight(line.String(), " ")
		if activeCodes != "" && !strings.HasSuffix(text, ansiReset) {
			text += ansiReset
		}
		result = append(result, text)
		line.Reset()
		if activeCodes != "" {
			line.WriteString(activeCodes)
		}
		lineWidth = 0
		lastBreakByte = -1
		activeAtBreak = ""
	}

	for index := 0; index < len(input); {
		if input[index] == '\x1b' && index+1 < len(input) && input[index+1] == '[' {
			end := index + 2
			for end < len(input) && !((input[end] >= 'A' && input[end] <= 'Z') || (input[end] >= 'a' && input[end] <= 'z')) {
				end++
			}
			if end < len(input) {
				end++
				sequence := input[index:end]
				line.WriteString(sequence)
				if sequence == ansiReset {
					activeCodes = ""
				} else if strings.HasSuffix(sequence, "m") {
					activeCodes += sequence
				}
				index = end
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(input[index:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		index += size
		if r == '\n' {
			flush(true)
			continue
		}

		runeWidth := runeDisplayWidth(r)
		if lineWidth > 0 && lineWidth+runeWidth > width {
			if lastBreakByte >= 0 {
				current := line.String()
				first := strings.TrimRight(current[:lastBreakByte], " ")
				if activeAtBreak != "" && !strings.HasSuffix(first, ansiReset) {
					first += ansiReset
				}
				result = append(result, first)
				remainderStart := lastBreakByte + 1
				for remainderStart < len(current) && current[remainderStart] == ' ' {
					remainderStart++
				}
				remainder := current[remainderStart:]
				line.Reset()
				if activeAtBreak != "" {
					line.WriteString(activeAtBreak)
				}
				line.WriteString(remainder)
				lineWidth = visibleWidth(remainder)
				lastBreakByte = -1
				activeAtBreak = ""
			} else {
				flush(false)
			}
			if r == ' ' && lineWidth == 0 {
				continue
			}
		}

		before := line.Len()
		line.WriteRune(r)
		lineWidth += runeWidth
		if r == ' ' {
			lastBreakByte = before
			activeAtBreak = activeCodes
		}
	}
	if line.Len() > 0 || len(result) == 0 {
		flush(true)
	}
	return result
}

func visibleWidth(text string) int {
	width := 0
	for index := 0; index < len(text); {
		if text[index] == '\x1b' && index+1 < len(text) && text[index+1] == '[' {
			index += 2
			for index < len(text) && !((text[index] >= 'A' && text[index] <= 'Z') || (text[index] >= 'a' && text[index] <= 'z')) {
				index++
			}
			if index < len(text) {
				index++
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(text[index:])
		if size == 0 {
			break
		}
		width += runeDisplayWidth(r)
		index += size
	}
	return width
}

func runeDisplayWidth(r rune) int {
	if r == 0 || r == '\n' || r == '\r' {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if r < 32 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	return (r >= 0x1100 && r <= 0x115f) ||
		(r >= 0x2329 && r <= 0x232a) ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) ||
		(r >= 0x20000 && r <= 0x3fffd)
}

func DetectWidth() int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && value > 0 {
		return value
	}
	return 96
}

func ShouldUseColor(mode string, writer *os.File) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "on", "yes", "true", "1":
		return true
	case "never", "off", "no", "false", "0":
		return false
	}
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	if writer == nil {
		return false
	}
	info, err := writer.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if relative, err := filepath.Rel(home, path); err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.Join("~", relative)
		}
	}
	return path
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.In(time.Local).Format("2006-01-02 15:04:05 MST")
}

func displayAPITime(parsed *time.Time, raw string) string {
	if parsed != nil {
		return formatTime(*parsed)
	}
	return emptyDash(raw)
}

func countCredit(count int, p palette) string {
	if count <= 0 {
		return p.gray(strconv.Itoa(count))
	}
	return p.magenta(p.bold(strconv.Itoa(count)))
}

func creditStatus(credit core.ResetCreditReport, p palette) string {
	switch strings.ToLower(credit.Status) {
	case "available":
		return p.green(credit.StatusLabel)
	case "expired", "revoked", "cancelled", "canceled":
		return p.red(credit.StatusLabel)
	case "redeeming", "in_progress", "processing":
		return p.yellow(credit.StatusLabel)
	default:
		return p.gray(credit.StatusLabel)
	}
}

func urgencyMarker(urgency string, p palette, glyph glyphSet) string {
	switch urgency {
	case "ends_today", "expires_soon", "expired":
		return p.red(glyph.warn)
	case "this_week":
		return p.yellow(glyph.warn)
	case "available":
		return p.green(glyph.ok)
	default:
		return p.gray(glyph.bullet)
	}
}

func urgencyLabel(credit core.ResetCreditReport, p palette) string {
	label := "[" + credit.UrgencyLabel + "]"
	switch credit.Urgency {
	case "ends_today", "expires_soon", "expired":
		return p.red(label)
	case "this_week":
		return p.yellow(label)
	case "available":
		return p.green(label)
	default:
		return p.gray(label)
	}
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func timeDurationSeconds(seconds int64) time.Duration {
	return time.Duration(seconds) * time.Second
}
