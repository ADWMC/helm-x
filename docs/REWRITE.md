# 重写说明 —— Wails v3 + Go + Vue 3

本仓库是 helm-x 的**重写版**（旧版 C++ 见 [`legacy`](https://github.com/ADWMC/helm-x/tree/legacy) 分支）。本文档收纳重写相关的全部内容：为什么重写、怎么重写、与旧版差在哪、设计文档在哪。产品说明与使用教程在 [README](../README.md)。

---

## 为什么重写

旧版（C++）的四个关键行为推断里有两个被实测推翻——**不能从源码读出行为**。重写前先做了 Phase A 实证（真实 codex 流量抓包、真实上游对照），再按实测结论重新设计机制，而不是逐文件移植：

- **P1（5xx 当成功）**：实测推翻——旧版正确透传 502（proxy.cpp:1093-1095）
- **P4（字段重排破坏搜索）**：实测推翻——问题面不存在
- **P2（拒绝时删除模型原话）**：实测证实——216 字节响应里只剩标记词，模型说的话整个被丢掉（FINDINGS A-2）
- **P3（SSE 全量缓冲）**：实测证实——`starttransfer≈total`，流式形同虚设

**Context Gardener** 也在实证中被判死：真实 codex 0.159.x 请求里裁剪目标（`function_call_output` / `custom_tool_call_output`）出现次数为 0，功能空转——重写版直接删除。

实证记录见 `docs/FINDINGS.md`、`docs/FINDINGS-phase1.md`。

## 与旧版的差别

| 项 | 旧版（legacy，C++） | 本版（Wails v3 + Go + Vue 3） |
|---|---|---|
| 界面 | 浏览器 WebUI（:8090） | 原生桌面窗口（WebView2），无 WebView2 时降级浏览器 |
| 拒绝处理 | **替换**模型原话，只留标记 | **附加**标记，原话完整保留 |
| Context Gardener | 有 | **删除**（实测裁剪目标不存在，功能空转） |
| 请求头 | 挑选转发 | 逐跳头之外全透传（UA / Cookie / X-\*） |
| 注入频率 | 每个请求注入 | 可配（每 N 次注入 1 次，按会话计数，失败不占名额） |
| JSON 处理 | 字符串搜索 | 字节级读写（jsonwalk），不重排未涉及字段 |
| 打包 | 单 exe | 单 exe + NSIS 安装包 |

不变量（实现约束）：

- **INV-6**：codex 配置改写字节精确、可原样还原
- **INV-8**：绝不删除模型原有输出（补救只做"附加"）
- **INV-9**：不认识的请求结构原样透传，不猜

## 技术栈

- **后端**：Go + Wails v3（桌面壳）、`internal/protocol/jsonwalk`（字节级 JSON）、`internal/protocol/sse`（SSE 分帧）
- **前端**：Vue 3 + Vite + Tailwind v4 + daisyUI（暖黑主题）
- **配置注入**：`internal/codexcfg` 行级 TOML 模型（自建，见 ADR-001）
- **测试**：Go 单测/集成（假上游脚本化）、vitest、破甲题库套件（`tests/helm-x-test-suite.py`）

## 设计文档索引

| 文档 | 内容 |
|---|---|
| `docs/PLAN.md` | 机制重设计总案 |
| `docs/PLAN-SPEC.md` | 数据模型、接口签名、事件契约、CLI 规格 |
| `docs/DESIGN.md` | UI/UX 设计体系（含设计系统 v4） |
| `docs/PHASES.md` | 阶段执行记录 |
| `docs/FINDINGS.md` / `FINDINGS-phase1.md` | 实证结论（含被推翻的推断） |
| `docs/RISKS.md` / `docs/ROADMAP.md` | 风险与路线 |
| `docs/adr/ADR-001-toml-handling.md` | 自建 TOML 模型的取舍 |
| `tests/TEST_STANDARD.md` | 破甲题库测试标准 |
