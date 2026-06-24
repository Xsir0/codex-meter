# Homebrew Setup

[中文](#中文说明)

The user-facing install command is:

```bash
brew tap Xsir0/Xsir0-homebrew-tap https://github.com/Xsir0/Xsir0-homebrew-tap.git
brew trust xsir0/xsir0-homebrew-tap
brew install codex-meter
```

For that command to work, `Xsir0/Xsir0-homebrew-tap` must exist and contain:

```text
Formula/codex-meter.rb
```

## What Is Already Automated

This repository now generates `release/codex-meter.rb` during every tagged release. The formula is also uploaded as a GitHub Release asset.

If the repository secret `HOMEBREW_TAP_TOKEN` exists, the release workflow also copies that formula into `Xsir0/Xsir0-homebrew-tap` and pushes it automatically.

## One-Time Setup

The tap repository is:

```text
https://github.com/Xsir0/Xsir0-homebrew-tap.git
```

Because this repository is not named `homebrew-tap`, users need the two-argument `brew tap` command shown above. Homebrew 6 also requires trusting third-party taps before loading formulae from them.

To enable automatic updates, create a GitHub fine-grained personal access token with write access to `Xsir0/Xsir0-homebrew-tap`, and add it to `Xsir0/codex-meter` as an Actions secret:

```text
HOMEBREW_TAP_TOKEN
```

After that, each new `v*` tag in this repository will publish the release and update the tap formula.

## Manual Fallback

If you do not want to configure the secret yet, download `codex-meter.rb` from the latest release and commit it manually to:

```text
Xsir0/Xsir0-homebrew-tap/Formula/codex-meter.rb
```

Then users can install with:

```bash
brew tap Xsir0/Xsir0-homebrew-tap https://github.com/Xsir0/Xsir0-homebrew-tap.git
brew trust xsir0/xsir0-homebrew-tap
brew install codex-meter
```

## 中文说明

用户最终使用的安装命令是：

```bash
brew tap Xsir0/Xsir0-homebrew-tap https://github.com/Xsir0/Xsir0-homebrew-tap.git
brew trust xsir0/xsir0-homebrew-tap
brew install codex-meter
```

这个命令要求 GitHub 上存在 `Xsir0/Xsir0-homebrew-tap` 仓库，并且里面有：

```text
Formula/codex-meter.rb
```

本项目已经会在每次 tag release 时生成 `release/codex-meter.rb`，并把它上传到 GitHub Release。

如果你在 `Xsir0/codex-meter` 仓库里添加了 Actions secret：

```text
HOMEBREW_TAP_TOKEN
```

release workflow 还会自动把公式推送到 `Xsir0/Xsir0-homebrew-tap`。

当前 tap 仓库是：

```text
https://github.com/Xsir0/Xsir0-homebrew-tap.git
```

因为仓库名不是 `homebrew-tap`，用户需要使用上面的两参数 `brew tap` 命令。Homebrew 6 还要求先 trust 第三方 tap，才能加载里面的 formula。

然后创建一个对 `Xsir0/Xsir0-homebrew-tap` 有写权限的 GitHub fine-grained personal access token，并把它保存到 `Xsir0/codex-meter` 的 Actions secrets，名字为 `HOMEBREW_TAP_TOKEN`。

如果暂时不想配 token，也可以从最新 release 下载 `codex-meter.rb`，手动提交到：

```text
Xsir0/Xsir0-homebrew-tap/Formula/codex-meter.rb
```
