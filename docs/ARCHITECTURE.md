# Architecture

```text
Claude Code
  ~/.claude/settings.json
  ANTHROPIC_BASE_URL=http://127.0.0.1:1800
             |
             v
helm-x proxy
  1. parse Anthropic Messages API request
  2. prune oversized historical tool_result content
  3. prepend the embedded Claude system prompt
  4. forward Anthropic headers and request body
  5. inspect the buffered JSON or SSE response
             |
             v
Anthropic API relay
```

## Components

| Component | Responsibility |
|---|---|
| `config.cpp` | Validate and update Claude Code `settings.json`; create and restore backups |
| `proxy.cpp` | Handle `/v1/messages`, system prompt injection, forwarding and response inspection |
| `rewrite.cpp` | Optional context-aware request rewriting and local fallback rules |
| `tamper.cpp` | Refusal-pattern matching |
| `resources.cpp` | Decrypt embedded prompts, dashboard, QA data and rules |
| `ui.cpp` | Local WebUI API |
| `verify.cpp` | Settings, resource and Claude CLI checks |
| `watch.cpp` | Periodic proxy-setting repair |

## Settings Lifecycle

```text
settings.json
  | apply
  +--> settings.json.helmx-bak
  | proxy start
  +--> settings.json.helmx-proxy-bak
  | proxy exit / --restore
  +<-- restore proxy backup
  | remove
  +<-- restore apply backup
```

All writes are JSON-validated and use a temporary file followed by an atomic replacement. The configuration layer never searches for or opens `.codex`.

## Protocol

The proxy accepts Anthropic Messages API bodies. User text is read from `messages[].content[]` blocks with `type: "text"`. The embedded prompt is prepended to the top-level `system` string or content-block array. Streaming requests remain streaming; buffered SSE is returned with `text/event-stream`.

The proxy forwards `Authorization`, `x-api-key`, `anthropic-version`, `anthropic-beta`, `User-Agent`, and `Accept`. Hop-by-hop and compression headers are rebuilt locally.
