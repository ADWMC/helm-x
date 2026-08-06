# helm-x

Codex CLI 环境控制工具 · 单二进制 · C++17 · 零外部 DLL 依赖

本地映射（Local Mapping）中间层：让 Codex CLI 走本地代理，请求时注入自定义指令（AGENTS），并对响应做 TAMPER 改写。所有资产加密内嵌，静态链接。

## 功能

- **本地映射代理**：`codex → 127.0.0.1:1800 → 上游 relay`（上游自动从 config 探测）
  - 请求时强制注入 AGENTS 指令（支持 codex 0.146 `input[]` 格式）
  - TAMPER 规则改写响应（拒绝 → 合规标记）
  - 强制 `stream=false`，避免 SSE 流阻塞（原 Python 版 bug）
  - 退出自动还原 base_url；强杀后 `--restore` 或 UI 一键还原
- **Web 控制台**（内嵌 HTML，加密存储）：状态 / 验证 / 服务（watch、proxy 三态）/ 规则
- **自愈守护 watch**：注入项被覆盖自动恢复
- **内置 MCP 服务器**：initialize / tools/list / tools/call（verify/activate/status/watch）
- **文件日志**：`~/.codex/helmx.log`，CLI 窗口实时流 + UI 查看

## 快速开始

```bat
helmx.exe            :: 双击：自动启动 proxy + Web 控制台 + 浏览器
helmx proxy --listen 1800   :: 本地映射（上游自动读 config）
helmx ui             :: Web 控制台 http://127.0.0.1:8090
helmx apply          :: 部署 AGENTS.md + config 注入
helmx activate       :: 发送激活词验证
helmx verify --e2e   :: 完整性自检（含真实 codex 验证）
helmx watch 60       :: 自愈守护
helmx proxy --restore :: 还原 base_url（强杀后）
```

## 目录结构

```
src/
  main.cpp       CLI 入口 + 双击启动（proxy + ui + browser）
  proxy.cpp      HTTP MITM（WinHTTP 上游、注入、TAMPER、stream=false）
  ui.cpp         Web 控制台（内嵌 HTML、API、watch/proxy 状态）
  http.cpp       极简 HTTP/1.1 服务器
  mcp.cpp        MCP stdio 服务器（Content-Length 帧协议）
  watch.cpp      自愈守护（原子标志后台线程）
  verify.cpp     自检 + 激活验证
  config.cpp     config.toml 合并注入 + 备份 + 还原
  resources.cpp  加密资源解密层
  tamper.cpp     TAMPER 规则引擎
  log.cpp        线程安全文件日志
tools/embed.py   资源加密生成器（XOR 流密钥）
assets/          AGENTS.md / dashboard.html / 规则（构建时加密内嵌）
```

## 构建

```bat
cmake -B build -G "MinGW Makefiles" -DCMAKE_BUILD_TYPE=Release
cmake --build build
:: 依赖：仅系统库 KERNEL32/UCRT/WS2_32/SHELL32/WINHTTP
```

## 日志

```
[18:35:15] [INFO] proxy: POST /v1/responses [INJECT] 76976B -> 69878B
[18:35:56] [INFO] proxy: upstream 200 (135425B)
[18:31:05] [INFO] req POST /api/apply -> 200
```

高频轮询 GET 静默，只记录操作事件（注入、上游状态、错误）。

## 设计要点

- 单二进制，资源 XOR 加密内嵌（strings 无明文）
- `stream=false` 强制：原 Python MITM 用 `urlopen().read()` 吞 SSE 流导致假超时，本实现完整规避
- config 写前备份 `.helmx-bak` / `.helmx-proxy-bak`，TOML 校验失败自动回滚
- MCP 用 `_setmode(_O_BINARY)` 保证 CRLF 帧协议在 Windows 下正确

## License

MIT
