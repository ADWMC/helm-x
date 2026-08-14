# helm-x - Claude Code Local Mapping Tool

**A local proxy controller for Claude Code**, shipped as a C++17 Windows x64 binary.

[中文](README.md) | [Changelog](CHANGELOG.md) | [FAQ](docs/FAQ.md)

## How It Works

helm-x runs between Claude Code and an Anthropic API relay:

```text
Claude Code -> http://127.0.0.1:1800 -> Anthropic API relay
```

The proxy handles Anthropic Messages API (`/v1/messages`) requests, injects an embedded system prompt, and provides request rewriting, response inspection, Context Gardener, and a WebUI.

helm-x only reads and writes Claude Code files:

```text
~/.claude/settings.json
~/.claude/settings.json.helmx-bak
~/.claude/settings.json.helmx-proxy-bak
~/.claude/helmx.log
```

It does not access `~/.codex`.

## Requirements

- Windows 10/11 x64
- Claude Code 2.x
- A working `ANTHROPIC_BASE_URL` and credential in Claude Code
- CMake 3.16+ and a C++17 MinGW-w64 or Visual Studio toolchain for builds

Example Claude Code configuration:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://your-relay.example",
    "ANTHROPIC_AUTH_TOKEN": "your-token"
  }
}
```

## Usage

Double-click `helmx.exe` to start the proxy and WebUI, then run `claude` in a new terminal. The original `ANTHROPIC_BASE_URL` is restored when helm-x exits normally.

CLI flow:

```powershell
helmx apply
helmx proxy --listen 1800
claude
```

Commands:

```text
helmx apply          Point Claude Code at 127.0.0.1:1800
helmx verify         Check settings, backups, and embedded resources
helmx verify --e2e   Also run claude -p "helmx"
helmx activate       Send the activation word through Claude Code
helmx proxy          Start the Anthropic Messages API proxy
helmx proxy --restore
                     Restore the pre-proxy settings
helmx ui             Start the WebUI
helmx watch          Periodically restore the proxy setting
helmx remove         Restore settings from before apply
```

Use `CLAUDE_CONFIG_DIR` to select another Claude Code profile.

## Configuration Safety

`apply` validates and backs up `settings.json`, then changes only `env.ANTHROPIC_BASE_URL`. Other Claude settings and environment variables are preserved. Invalid JSON is left unchanged.

`proxy` stores its temporary restore point in `settings.json.helmx-proxy-bak`. `remove` restores the full `settings.json.helmx-bak` created before `apply`.

## Build and Test

```powershell
cmake -S . -B build -G "MinGW Makefiles"
cmake --build build -j
$env:HELMX_TEST_BIN = (Resolve-Path "build\helmx.exe")
python -m unittest tests.test_config tests.test_ui tests.test_embed -v
```

## License

[MIT](LICENSE)
