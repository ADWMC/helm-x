# helm-x

Codex CLI 环境控制工具（C++17，单二进制）。

## 子命令

- `apply` — 部署 AGENTS.md + config 合并注入
- `watch` — 自愈守护：定时校验注入项完整性，被覆盖自动恢复
- `proxy` — 篡改代理：HTTP MITM 注入 + 拒绝响应篡改
- `mcp` — 内置 MCP 服务器（stdio JSON-RPC）
- `remove` — 卸载恢复

## 构建

```bat
cmake -B build -G "MinGW Makefiles" -DCMAKE_BUILD_TYPE=Release
cmake --build build
```

## 设计

- 零外部 DLL 依赖（静态链接）
- 资产（AGENTS.md / TAMPER 规则 / MCP 工具定义）加密内嵌资源
- 运行时解密到内存，不落盘明文
