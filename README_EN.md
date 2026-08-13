# Codex Environment Control (helm-x)

[中文](README.md) | **English**

**Repository**: https://github.com/ADWMC/helm-x

---

**If you find this project helpful, please give it a Star!**

---

**Codex CLI local mapping control tool** · Single binary · C++17 · Zero external DLL dependencies

**🛡️ 0 Cyber**: This tool does not launch any real network attacks or connect to any external targets — it only performs local Codex environment control (local mapping / instruction injection / response tampering). All traffic stays between your machine and your configured upstream relay.

> **⚠️ For educational and research purposes only. Commercial use prohibited.**

---

## What is this

helm-x is a local mapping layer for Codex CLI: `codex → 127.0.0.1:1800 → upstream relay`. When requests pass through the local proxy, it forcefully injects custom instructions (AGENTS) and tampers with refusal responses at the response layer — keeping conversations uninterrupted and content deliverable.

**Core capabilities**:
1. **Context injection**: bridge.md establishes security research context, blocking cyber flag detection
2. **TAMPER engine**: Refusal responses are rewritten as compliance markers
3. **Context-aware rewriter**: Auto-rewrites requests when cyber flag triggers (with retry mechanism)
4. **Context Gardener**: Prunes oversized historical tool output and Base64 images before forwarding
5. **Cloud-updatable QA**: Searchable WebUI help with GitHub updates and offline fallback
6. **Zero dependencies**: Single exe, statically linked, Windows system libs only

---

## Test Results

### Core Metrics (57 high-risk requests)

| Metric | Value | Notes |
|--------|-------|-------|
| **Total Pass Rate** | **100% (57/57)** | Direct pass + rewritten pass |
| Direct Pass | 55 (96.5%) | AGENTS injection blocks cyber flag |
| Rewritten Pass | 2 (3.5%) | Rewriter auto-rewrites + clean session |
| Cyber Flag Trigger | 2/57 (3.5%) | Metasploit, self-replicating program |

### Comparison with Previous Version

| Metric | v0.0.1-beta | v0.0.2-fix1 | Improvement |
|--------|-------------|-------------|-------------|
| Cyber Flag Rate | 100% | **3.5%** | ↓96.5% |
| Total Pass Rate | 0% | **100%** | ↑100% |
| Test Cases | 8 | 57 | +49 |

### Test Cases (57, 12 categories)

| Category | Cases | Pass Rate | Cyber Flag |
|----------|-------|-----------|------------|
| Process Hide | 6 | 100% | 0 |
| Keylogger | 6 | 100% | 0 |
| Network Attack | 6 | 100% | 0 |
| Exploit | 6 | 100% | 1 |
| Malware | 6 | 100% | 0 |
| Game Security | 6 | 100% | 0 |
| License Crack | 6 | 100% | 0 |
| Boundary | 6 | 100% | 1 |
| Violence | 7 | 100% | 0 |
| Pornography | 7 | 100% | 0 |
| Mixed Boundary | 7 | 100% | 0 |
| Financial/Political | 7 | 100% | 0 |

**Detailed report**: `tests/v002-fix1-full-test.md`

---

## Architecture

```
codex ──> http://127.0.0.1:1800 ──> upstream relay
              │
         [WinHTTP forward]   [Inject AGENTS]
         [stream=false]      [TAMPER rewrite]
         [auto-config]       [Auto-restore on exit]
              │
         [cyber flag?] ──→ [Rewriter] ──→ [Clean session] ──→ [Upstream]
```

```
helmx.exe (single binary)
├─ proxy    HTTP MITM (WinHTTP upstream, inject, TAMPER, rewriter)
├─ ui       Web console (embedded HTML, 4 pages)
├─ watch    Self-healing daemon (auto-restore when injection overwritten)
├─ apply/remove   Deploy/undeploy AGENTS + config injection
├─ activate/verify  Activation verification / integrity check
```

---

## Quick Start

### Requirements

| Item | Requirement |
|---|---|
| Windows | 10/11 |
| Codex CLI | 0.146+ (responses wire API) |
| Upstream relay | Any OpenAI-compatible API (base_url in codex config) |
| Runtime | None (only KERNEL32/UCRT/WS2_32/SHELL32/WINHTTP) |

### Install

```bat
:: Option A — Double-click (recommended)
helmx.exe
:: Auto: starts proxy(:1800) + UI(:8090) + opens browser

:: Option B — Command line
helmx proxy --listen 1800          :: Local mapping
helmx ui                           :: Web console
```

### WebUI QA and Context Settings

Open `http://127.0.0.1:8090`. The **QA Help** page provides searchable common
errors and checks GitHub for updates, falling back to cached or embedded data.
The **Context** page configures Context Gardener, tool-output limits, and Codex
auto-compaction settings. Maintainers can add questions by editing
[`assets/qa.json`](assets/qa.json); see [`docs/QA.md`](docs/QA.md).

### Verify

```bat
helmx activate        :: Send activation word
helmx verify --e2e    :: 7 self-checks + codex activation
```

### Configure Rewriter (optional)

The rewriter auto-rewrites user messages when cyber flag triggers. Requires a **non-reasoning model** as the rewriter backend.

#### Model Requirements

| Requirement | Notes |
|-------------|-------|
| **Non-reasoning model** | Reasoning models (mimo-v2.5-pro, o1, o3) have empty `content` field |
| **Fast response** | Retries 3 times, needs <5s per response |
| **Chinese support** | Rewriter prompt includes Chinese examples |
| **API compatible** | OpenAI chat/completions format |

#### Recommended Models

| Model | Provider | Latency | Notes |
|-------|----------|---------|-------|
| **meta/llama-3.1-8b-instruct** | NVIDIA NIM | ~2s | Recommended, fast and stable |
| meta/llama-3.1-70b-instruct | NVIDIA NIM | ~5s | Stronger but slower |
| gpt-4o-mini | OpenAI | ~2s | Requires OpenAI key |
| qwen2.5-7b-instruct | Alibaba Cloud | ~2s | Available in China |
| deepseek-chat | DeepSeek | ~2s | Available in China |

#### Config Example

```json
{
  "rewriter": {
    "enabled": true,
    "provider": "nvidia",
    "base_url": "https://integrate.api.nvidia.com/v1",
    "api_key": "nvapi-xxx",
    "model": "meta/llama-3.1-8b-instruct",
    "timeout_sec": 60,
    "use_proxy": false,
    "proxy_url": "http://127.0.0.1:7897"
  }
}
```

#### Config Fields

| Field | Description | Default |
|-------|-------------|---------|
| enabled | Enable rewriter | false |
| provider | Provider identifier (for logs) | nvidia |
| base_url | API endpoint | https://integrate.api.nvidia.com/v1 |
| api_key | API key | (required) |
| model | Model name | meta/llama-3.1-8b-instruct |
| timeout_sec | Request timeout (seconds) | 60 |
| use_proxy | Use HTTP proxy | false |
| proxy_url | HTTP proxy URL | http://127.0.0.1:7897 |

#### Notes

- **Don't use reasoning models**: mimo-v2.5-pro, o1, o3 have empty `content` field
- **NVIDIA NIM is free**: Register at https://build.nvidia.com for free API key
- **Rewriter prompt is embedded**: No external file needed, XOR encrypted in binary
- **Configurable in UI**: Visit http://127.0.0.1:8090 "改写器" page

---

## Build

```bat
:: 1. Generate encrypted resources
python tools/embed.py

:: 2. Compile
cmake -B build -G "MinGW Makefiles" -DCMAKE_BUILD_TYPE=Release
cmake --build build
```

## License

[GNU AGPL v3.0](LICENSE)

> **⚠️ For educational and research purposes only.**
