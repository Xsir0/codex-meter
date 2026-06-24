package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Xsir0/codex-meter/internal/core"
	"github.com/Xsir0/codex-meter/internal/ui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type options struct {
	authFile    string
	codexHome   string
	timeout     time.Duration
	watch       time.Duration
	retries     int
	jsonOutput  bool
	rawOutput   bool
	showAccount bool
	ascii       bool
	color       string
	noColor     bool
	width       int
	showVersion bool
	helpOnly    bool
}

type rawEnvelope struct {
	FetchedAt    time.Time          `json:"fetched_at"`
	Status       string             `json:"status"`
	AccountID    string             `json:"account_id,omitempty"`
	Usage        json.RawMessage    `json:"usage,omitempty"`
	ResetCredits json.RawMessage    `json:"reset_credits,omitempty"`
	Errors       []core.ReportError `json:"errors,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr *os.File) int {
	configuration, parseCode := parseFlags(arguments, stderr)
	if parseCode != 0 {
		return parseCode
	}
	if configuration.helpOnly {
		return 0
	}
	if configuration.showVersion {
		fmt.Fprintf(stdout, "codex-meter %s (%s, %s, %s/%s)\n", version, commit, date, runtime.GOOS, runtime.GOARCH)
		return 0
	}
	if message := validateOptions(configuration); message != "" {
		fmt.Fprintln(stderr, "Invalid arguments: "+message)
		fmt.Fprintln(stderr, "Run codex-meter --help for usage.")
		return 64
	}

	authPath, err := core.ResolveAuthPath(configuration.authFile, configuration.codexHome)
	if err != nil {
		return renderStartupError(stdout, configuration, "Failed to resolve the credentials path: "+err.Error())
	}

	client := core.NewClient(configuration.timeout)
	client.MaxAttempts = configuration.retries + 1
	client.UserAgent = fmt.Sprintf("codex-meter/%s (%s/%s)", version, runtime.GOOS, runtime.GOARCH)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if configuration.watch <= 0 {
		code, outputErr := executeOnce(ctx, client, authPath, configuration, stdout, false)
		if outputErr != nil {
			if !isBrokenPipe(outputErr) {
				fmt.Fprintln(stderr, "Output failed: "+outputErr.Error())
			}
			return 1
		}
		return code
	}

	return executeWatch(ctx, client, authPath, configuration, stdout, stderr)
}

func parseFlags(arguments []string, stderr io.Writer) (options, int) {
	var configuration options
	set := flag.NewFlagSet("codex-meter", flag.ContinueOnError)
	set.SetOutput(stderr)
	set.StringVar(&configuration.authFile, "auth-file", "", "Path to Codex auth.json (highest priority)")
	set.StringVar(&configuration.codexHome, "codex-home", "", "Codex home directory; defaults to CODEX_HOME or ~/.codex")
	set.DurationVar(&configuration.timeout, "timeout", 12*time.Second, "Timeout for each HTTP request")
	set.DurationVar(&configuration.watch, "watch", 0, "Refresh interval, for example 5m; disabled by default")
	set.IntVar(&configuration.retries, "retries", 1, "Extra retries for network errors or 5xx responses (0-3)")
	set.BoolVar(&configuration.jsonOutput, "json", false, "Print normalized JSON")
	set.BoolVar(&configuration.rawOutput, "raw", false, "Print raw JSON from both endpoints (credentials excluded)")
	set.BoolVar(&configuration.showAccount, "show-account-id", false, "Show the full Account ID and email; masked by default")
	set.BoolVar(&configuration.ascii, "ascii", false, "Use ASCII-only borders and progress bars")
	set.StringVar(&configuration.color, "color", "auto", "Color mode: auto, always, or never")
	set.BoolVar(&configuration.noColor, "no-color", false, "Disable color (same as --color never)")
	set.IntVar(&configuration.width, "width", 0, "Dashboard width; defaults to COLUMNS, range 58-132")
	set.BoolVar(&configuration.showVersion, "version", false, "Show version information")
	set.Usage = func() {
		fmt.Fprintln(stderr, "codex-meter — view Codex, Spark, and reset-credit quotas")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Usage:")
		fmt.Fprintln(stderr, "  codex-meter [options]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Examples:")
		fmt.Fprintln(stderr, "  codex-meter")
		fmt.Fprintln(stderr, "  codex-meter --watch 5m")
		fmt.Fprintln(stderr, "  codex-meter --json")
		fmt.Fprintln(stderr, "  codex-meter --raw > codex-usage.json")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Options:")
		set.PrintDefaults()
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Exit codes: 0=complete, 2=partial, 1=failed, 64=invalid arguments.")
	}
	if err := set.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			configuration.helpOnly = true
			return configuration, 0
		}
		return configuration, 64
	}
	if set.NArg() > 0 {
		fmt.Fprintf(stderr, "Unexpected positional argument(s): %s\n", strings.Join(set.Args(), " "))
		return configuration, 64
	}
	return configuration, 0
}

func validateOptions(configuration options) string {
	if configuration.jsonOutput && configuration.rawOutput {
		return "--json and --raw cannot be used together"
	}
	color := strings.ToLower(strings.TrimSpace(configuration.color))
	if configuration.noColor {
		color = "never"
	}
	if color != "auto" && color != "always" && color != "never" {
		return "--color must be auto, always, or never"
	}
	if configuration.timeout < time.Second || configuration.timeout > 2*time.Minute {
		return "--timeout must be between 1s and 2m"
	}
	if configuration.watch > 0 && configuration.watch < 10*time.Second {
		return "--watch must be at least 10s to reduce rate-limit risk"
	}
	if configuration.retries < 0 || configuration.retries > 3 {
		return "--retries must be between 0 and 3"
	}
	if configuration.width != 0 && (configuration.width < 58 || configuration.width > 132) {
		return "--width must be between 58 and 132"
	}
	return ""
}

func executeOnce(ctx context.Context, client *core.Client, authPath string, configuration options, stdout *os.File, compactJSON bool) (int, error) {
	now := time.Now()
	auth, err := core.LoadAuth(authPath, now)
	if err != nil {
		report := failedReport(authPath, err, now)
		return 1, outputReport(stdout, report, core.Snapshot{}, configuration, compactJSON)
	}

	snapshot := client.FetchAll(ctx, auth)
	report := core.BuildReport(auth, snapshot, time.Now(), configuration.showAccount)
	code := 0
	if report.Status == "partial" {
		code = 2
	} else if report.Status == "failed" {
		code = 1
	}
	return code, outputReport(stdout, report, snapshot, configuration, compactJSON)
}

func executeWatch(ctx context.Context, client *core.Client, authPath string, configuration options, stdout, stderr *os.File) int {
	interactive := isTerminal(stdout) && !configuration.jsonOutput && !configuration.rawOutput
	first := true
	lastCode := 0

	for {
		if interactive && !first {
			fmt.Fprint(stdout, "\x1b[H\x1b[2J")
		}
		code, err := executeOnce(ctx, client, authPath, configuration, stdout, configuration.jsonOutput || configuration.rawOutput)
		lastCode = code
		if err != nil {
			if !isBrokenPipe(err) {
				fmt.Fprintln(stderr, "Output failed: "+err.Error())
			}
			return 1
		}
		first = false

		// Start the interval after a refresh finishes. A slow network request must
		// not cause an immediate catch-up request that could trigger rate limiting.
		timer := time.NewTimer(configuration.watch)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			if interactive {
				fmt.Fprintln(stdout)
			}
			if lastCode == 1 {
				return 1
			}
			return 0
		case <-timer.C:
			if !interactive && !configuration.jsonOutput && !configuration.rawOutput {
				fmt.Fprintln(stdout)
				fmt.Fprintln(stdout, strings.Repeat("-", 72))
				fmt.Fprintln(stdout)
			}
		}
	}
}

func outputReport(stdout *os.File, report core.Report, snapshot core.Snapshot, configuration options, compact bool) error {
	if configuration.rawOutput {
		accountID := report.Auth.AccountID
		envelope := rawEnvelope{
			FetchedAt:    report.GeneratedAt,
			Status:       report.Status,
			AccountID:    accountID,
			Usage:        snapshot.UsageRaw,
			ResetCredits: snapshot.ResetCreditsRaw,
			Errors:       report.Errors,
		}
		return encodeJSON(stdout, envelope, compact)
	}
	if configuration.jsonOutput {
		return encodeJSON(stdout, report, compact)
	}
	colorMode := configuration.color
	if configuration.noColor {
		colorMode = "never"
	}
	return ui.Render(stdout, report, ui.Options{
		Color:       ui.ShouldUseColor(colorMode, stdout),
		Unicode:     !configuration.ascii,
		Width:       configuration.width,
		ShowAccount: configuration.showAccount,
	})
}

func encodeJSON(writer io.Writer, value any, compact bool) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if !compact {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func failedReport(authPath string, err error, now time.Time) core.Report {
	return core.Report{
		GeneratedAt: now,
		Status:      "failed",
		Auth: core.AuthReport{
			Path:             authPath,
			AccountID:        "—",
			AccountIDMasked:  true,
			CredentialStatus: "unavailable",
		},
		Errors: []core.ReportError{{Section: "auth", Message: err.Error(), Kind: "auth"}},
	}
}

func renderStartupError(stdout *os.File, configuration options, message string) int {
	report := failedReport("—", errors.New(message), time.Now())
	_ = outputReport(stdout, report, core.Snapshot{}, configuration, false)
	return 1
}

func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}
