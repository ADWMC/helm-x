# Changelog

## v0.0.2-beta (2026-08-07)

### New Features
- **Context-aware rewriter**: gptbypass-style rewriting with refusal text, 3x retry, forbidden word list
- **Clean session bypass**: cyber flag triggers history stripping + field-preserving request rebuild
- **Embedded rewrite prompt**: rewrite_prompt.txt XOR-encrypted in binary (no external file needed)
- **68-case test matrix**: cyber-test-matrix-v2.md covering 7 categories
- **Version management**: single source of truth in version.h

### Improvements
- AGENTS injection now blocks most cyber flags directly (8/8 high-risk requests pass)
- Rewriter loads from: config > embedded resource > external file
- Proxy port configurable via `proxy_url` in helmx.config.json (default: 127.0.0.1:7897)
- All version strings unified (CMake, MCP, User-Agent)
- README updated with latest test results and architecture

### Bug Fixes
- Fixed cyber retry re-injecting AGENTS (caused second trigger)
- Fixed clean session losing upstream fields (tools, reasoning)
- Fixed hardcoded proxy port in rewrite.cpp

### Credits
- [gptbypass](https://github.com/null119/gptbypass) — rewriter prompt and refusal detection
- [gpt-5.6-instruct](https://github.com/MDX-Tom/gpt-5.6-instruct) — testing methodology

## v0.0.1-beta (2026-08-06)

### Initial Release
- HTTP MITM proxy with AGENTS injection
- TAMPER engine for refusal rewriting
- Web dashboard (Clash Verge style)
- MCP server (6 built-in tools)
- Self-healing watch daemon
- XOR-encrypted embedded resources
- Single binary, zero external DLL dependencies

### Credits
- [NERV-BREAK-5.6](https://github.com/lingbol088-spec/5.6-JAILBREAK-NERV-codex-instruct-5.6)
- [Codex-X](https://github.com/yynxxxxx/Codex-X)
