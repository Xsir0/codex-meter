# codex-meter

[中文说明](README.zh-CN.md)

`codex-meter` is a small Go CLI that shows your ChatGPT Codex usage from the terminal.

It shows:

- Codex 5-hour and weekly quota windows
- Spark quota windows, when available
- Reset-credit count and details
- Masked account and authentication status

It is read-only. It does not redeem reset credits, change quotas, or print your access token.

![codex-meter terminal dashboard](assets/codex-meter-dashboard.png)

## Install

### macOS and Linux

```bash
curl -fsSL https://github.com/Xsir0/codex-meter/releases/latest/download/install.sh | sh
```

Install to a user-owned directory:

```bash
curl -fsSL https://github.com/Xsir0/codex-meter/releases/latest/download/install.sh | \
  sh -s -- --install-dir "$HOME/.local/bin"
```

Uninstall:

```bash
curl -fsSL https://github.com/Xsir0/codex-meter/releases/latest/download/install.sh | sh -s -- --uninstall
```

### Homebrew

```bash
brew install Xsir0/tap/codex-meter
```

The tap repository must be published first. See [Homebrew setup](docs/homebrew.md).

### Go

```bash
go install github.com/Xsir0/codex-meter/cmd/codex-meter@latest
```

### Build From Source

```bash
go build -trimpath -o codex-meter ./cmd/codex-meter
./codex-meter
```

## Before Use

`codex-meter` reuses your Codex CLI ChatGPT login. Log in first:

```bash
codex login
```

If the tool cannot find `~/.codex/auth.json`, configure Codex CLI to store credentials in a file, then log in again:

```toml
cli_auth_credentials_store = "file"
```

## Usage

```bash
codex-meter
```

Common commands:

```bash
codex-meter                     # Show the dashboard
codex-meter --watch 5m          # Refresh every five minutes
codex-meter --json              # Print normalized JSON
codex-meter --raw               # Print raw endpoint JSON for troubleshooting
codex-meter --ascii --no-color  # Plain text for logs
codex-meter --show-account-id   # Show full account ID and email
```

Useful options:

- `--auth-file PATH`: use a specific Codex `auth.json`
- `--codex-home PATH`: use another Codex home directory
- `--timeout 12s`: set request timeout
- `--width N`: set dashboard width
- `--version`: print version information

Run `codex-meter --help` for all options.

## License

MIT.
