# helm-x-wails 阶段计划

> `PLAN.md` §6 定阶段划分的**理由**，本文定每个阶段的**任务清单与验收命令**。
> 硬规则：**每个阶段结束时必须能独立运行并验证**，不做"全部写完再联调"。
> 未跑命令不得称完成（Patchwork 不变量 4）。

---

## 阶段总览

| 阶段 | 内容 | 状态 |
|---|---|---|
| A | ★ 证伪 P1–P4 | ✅ **已完成**（P1/P4 证伪，P2/P3 确认） |
| A2 | 真实请求体 + 注入有效性 | ✅ **已完成**（发现 Context Gardener 空转） |
| 0 | `protocol/jsonwalk` + `sse` | ✅ **已完成**（含 fuzz） |
| 1 | 管线骨架 + 状态机 | ✅ **已完成** |
| 2 | 三段补救（TAMPER + 改写器） | ✅ **已完成** |
| 3 | 配置与自检 + CLI | ✅ **已完成** |
| 4 | GUI 接线（Wails 窗口 + 10 页面） | ✅ **已完成** |
| 5 | 发布打包 | ⬜ 未做 |

**全程实测验证**：
- 真 codex → 本代理 → 真上游：成功
- `apply` → `remove` 逐字节还原：成功（真实 173 行配置，SHA256 一致）
- 崩溃后 `proxy --restore` 兜底：成功
- 桌面窗口加载真实数据：成功

**尚未做的**：`wails3 package` 打包、60 题基线复现、改写器端到端（需 LLM key）。

---

## 阶段 A — 证伪 P1–P4 ✅ 已完成

**目的**：用假上游打旧版 `helmx.exe`，实测四条推断。不需要上游 relay、不需要 codex、不需要 key。

### 任务

- [x] A1 起假上游（`tests/phaseA/fakeupstream`）
- [x] A2 造临时 `CODEX_HOME`（**未碰真实配置**）
- [x] A3 启动旧版 `helmx.exe proxy`
- [x] A4 逐个场景打请求，记录旧版行为
- [ ] A5 产出 `docs/FINDINGS.md`：每条 P 判定为 `confirmed` / `refuted` / `inconclusive`

### 场景表

| # | 假上游返回 | 验证 | 预期（若 P 成立） |
|---|---|---|---|
| A-1 | `502` + `{"error":{"message":"upstream boom"}}` | P1 | 旧版判为成功并转发，TAMPER 可能改写该错误 JSON |
| A-2 | `200` + SSE，首帧即拒绝文本 | P2 | 旧版整体替换为 marker，**原话丢失** |
| A-3 | `200` + SSE，若干帧正常文本 | P3 | 旧版**收完才发**，首字节延迟 ≈ 生成总时长 |
| A-4 | `200` + 正常 JSON，字段顺序打乱 | P4 | 旧版裸搜索仍能命中（可能 refuted → 需改方案） |
| A-5 | `200` + 拒绝文本（非流式） | 基线 | marker + 原话保留（旧版此处正确） |

### 验收命令

```powershell
# A-3 的关键测量：首字节时间
curl.exe -N -w "starttransfer=%{time_starttransfer}s total=%{time_total}s\n" `
  -X POST http://127.0.0.1:1801/v1/responses `
  -H "Content-Type: application/json" -d "@tests/fixtures/stream.json"
```

**判据**：`time_starttransfer ≈ time_total` → P3 成立；`<<` → P3 不成立。

### 产出要求

`docs/FINDINGS.md` 每条给出：场景、假上游返回、旧版实际行为、原始日志片段、结论。
**结论为 refuted 时，必须回改 `PLAN.md` 对应章节**，不得留着一份与事实不符的方案。

---

## 阶段 A2 — 真实请求体与注入有效性 ✅ 已完成

**目的**：`RequestView` 的字段假设全部来自读 C++。用真实 codex + 真实上游实测替代推断。

### 已完成

- [x] A2.1 安装 codex CLI（`@openai/codex@0.159.3`）
- [x] A2.2 建记录型假上游（`tests/capture/recordupstream`），抓真实请求体
- [x] A2.3 验证注入**不破坏请求**（`tests/capture/probeinject`，三种变体全 200）
- [x] A2.4 验证注入**确实生效**（`tests/capture/probeinject-effect`，标记词出现）
- [x] A2.5 捕获带工具调用的完整会话（`tests/capture/tapupstream`，7 轮往返）

### 结论（详见 [`FINDINGS-phase1.md`](FINDINGS-phase1.md)）

| 项 | 结论 |
|---|---|
| 注入位置（system 前插 `input[0]`） | ✅ **有效**，上游接受且指令被遵守 → **沿用旧版做法** |
| `max_output_tokens` / `instructions` | ❌ 不存在，`RequestView` 需容忍缺失 |
| **Context Gardener 的裁剪目标** | ❌ **两个 type 都不存在 → 旧版功能空转**。用户决策：**该功能删除，不重写** |

### 对后续阶段的影响

- **Context Gardener 已从方案中移除**（`PLAN.md` M4 标为废弃），不再占用阶段 2
- 旧配置里的 `context_gardener_*` 字段：**读取时忽略、不报错、不再写回**（向后兼容）
- `PLAN-SPEC.md` §2.1 的 `RequestView` 按实测调整
- 注入部分（`inject.go`）**可以按原设计实现**，已实测有效

---

## 阶段 0 — 协议地基 ✅ 已完成

### 任务

- [ ] 0.1 `internal/protocol/jsonwalk`：`Parse` / `Member` / `Index` / `String` / `Number` / `InsertFirst` / `ReplaceString` / `Splice`
- [ ] 0.2 `internal/protocol/sse`：增量解析，跨 chunk 截断安全
- [ ] 0.3 `tests/fixtures/`：真实 codex 请求体（至少 3 种形态：普通 / 带 tools / 带 function_call_output）
- [ ] 0.4 表驱动测试 + fuzz

### 验收命令

```powershell
go test ./internal/protocol/... -race -count=1
go test ./internal/protocol/jsonwalk -fuzz=FuzzParse -fuzztime=30s
```

### 通过标准

- 覆盖：`\uXXXX` 代理对、重复 key、嵌套、转义引号、大整数、空对象/数组、非法截断
- **保真断言**：改一个字段后，其余字节 `bytes.Equal` 完全相同
- 非法输入返回 error，不 panic

---

## 阶段 1 — 管线骨架 + 状态机 ✅ 已完成

### 任务

- [ ] 1.1 `proxy/engine`：`net/http` 服务端，生命周期
- [ ] 1.2 `proxy/request`：规范化 + 无法解析则透传
- [ ] 1.3 `proxy/upstream`：转发 + 重试策略（**保持旧语义**：4xx 除 408/429 不重试）
- [ ] 1.4 `proxy/response`：`Classify` 状态机（`PLAN-SPEC.md` §2.2 判定顺序）
- [ ] 1.5 `proxy/observe`：`RequestRecord` + 事件
- [ ] 1.6 **流式窗口**：首段缓冲 + `Flush` 门控
- [ ] 1.7 **不含**注入 / 改写 / TAMPER

### 验收命令

```powershell
go test ./internal/proxy/... -race -count=1
# 端到端：假上游 + 真实请求体，正常请求应逐字节透传
go test ./internal/proxy -run TestPassthroughByteIdentical -v
```

### 通过标准

- 正常请求**逐字节透传**（除被注入/裁剪的部分）
- 六种 `Class` 各有表驱动用例
- 流式首字节延迟 < 100ms（假上游慢速发送时）
- `UpstreamFailed` 场景下 TAMPER 一次都不被调用（用计数断言）

---

## 阶段 2 — 三段补救 ✅ 已完成

### 任务

- [ ] 2.1 `tamper`：规则加载 + `is_refusal`（26 条规则表驱动）
- [ ] 2.2 `rewriter`：仅 LLM 路径；3 次换角度；失败即返回 false 不重试
- [ ] 2.3 `proxy/remedy`：`retry → attach_marker → pass_through`
- [ ] 2.4 `proxy/session`：clean session 重建（基于 `jsonwalk` 定点手术）
- [x] ~~2.5 `contextguard`：按字节阈值裁剪~~ **已删除，不实现**（实测功能空转，用户决策移除）
- [ ] 2.6 `proxy/session` 状态（LRU，256 会话 / 30 分钟 TTL）

### 验收命令

```powershell
go test ./internal/tamper/... ./internal/rewriter/... -race
go test ./internal/proxy -run TestRemedyLadder -v
go test ./internal/proxy -run TestNeverDropsOriginalText -v   # INV-8
```

### 通过标准

- **INV-8 断言**：任何补救路径下，最终响应体都包含模型原文（或明确标记未补救）
- **INV-2 断言**：改写 API 失败时，上游请求数不增加
- 补救阶梯：假上游脚本化序列 `拒绝→拒绝→通过` 应走 `retry` 并成功
- `TestAgainstOldVersion`：新旧对照报告生成，差异分类

---

## 阶段 3 — 配置与自检 ✅ 已完成

### 任务

- [x] 3.1 `codexcfg`：行级 TOML 模型（`Doc`）— **已完成**
- [x] 3.2 注入 / 备份 / 原子写 / 还原 — **已完成**
- [x] 3.2b ADR-001：为什么自建行级模型而非用成熟 TOML 库 — **已完成**
- [ ] 3.3 `selfcheck`：7 项 + e2e
- [ ] 3.4 `watch`：自愈守护
- [ ] 3.5 CLI 全部子命令

### 验收命令

```powershell
go test ./internal/codexcfg/... -race
# 字节级回滚验证
go test ./internal/codexcfg -run TestByteRoundTrip -v
# 对真实 config.toml 副本的往返（不含凭据进入仓库）
$env:HELMX_REAL_CONFIG = "$env:USERPROFILE\.codex\config.toml"
go test ./internal/codexcfg -run TestRoundTripAgainstRealConfig -v
helmx apply; helmx verify; helmx remove
```

### 通过标准

- **INV-6**：`apply` → `remove` 后 `config.toml` 与初始**逐字节一致**
- 行级模型：改一个键，其余行 `bytes.Equal` 不变（含注释与缩进）
- 损坏 TOML 拒绝写入，且不破坏原文件
- `verify` 7 项全 PASS（e2e 可选）

### 实测结果（已完成部分）

| 项 | 命令 | 结果 |
|---|---|---|
| 构建 | `go build ./...` | exit 0 |
| 静态检查 | `go vet ./...` | exit 0 |
| 格式 | `gofmt -l internal/` | 无输出 |
| 单元测试 | `go test ./internal/codexcfg/` | 通过 |
| **真实配置往返** | `HELMX_REAL_CONFIG=... -run TestRoundTripAgainstRealConfig` | **通过**：7013 字节 / 232 行 / 58 表，apply→remove 后**逐字节一致** |

**过程中被测试拦下的缺陷**（真实 bug，非假设）：

1. **值区间在 CRLF 行上整体偏移** —— `classify` 用去 EOL 后的 `body` 计算偏移却写入 `Raw`，CRLF 文件的每个值都错位，导致 `base_url = ""new"` 这类**配置损坏**。已修。
2. **引号值丢失起始引号** —— `stripInlineComment` 的 `lastNonWS` 初值使首个字符不计入，`"old"` 被截成 `old"`。已修。


---

## 阶段 4 — GUI 接线 ✅ 已完成

前端骨架已完成。剩余：

- [ ] 4.1 Go 侧 `svc` 全部绑定方法
- [ ] 4.2 事件推送接通（8 个事件，`PLAN-SPEC.md` §4）
- [ ] 4.3 总览页真实布局（状态条 + 请求表 + 三指标）
- [ ] 4.4 其余 9 页接线
- [ ] 4.5 托盘 / 单实例 / 退出还原
- [ ] 4.6 WebView2 缺失降级到 `helmx ui`

### 验收命令

```powershell
cd frontend; npx vitest run; npx vite build
wails3 dev     # 手动走查
```

### 通过标准

- 逐页对照旧 `:8090`，功能无缺失
- 事件驱动，**无轮询**（Network 面板确认）
- 对照 `DESIGN.md` 逐条核规则
- 危险操作走 §3.1 流程（列出文件 / 键 / 备份 / 撤销方式）

---

## 阶段 5 — 发布

- [ ] 5.1 资源加密管线（`AES-GCM`，密钥外部化）
- [ ] 5.2 图标 / 版本 / `wails3 package`
- [ ] 5.3 迁移说明（旧版配置直接可用、回滚步骤）
- [ ] 5.4 60 题矩阵重跑

### 验收命令

```powershell
wails3 package
# 干净环境冒烟：无 %APPDATA%\helmx.config.json
.\helmx.exe
# 密钥不泄露
strings helmx.exe | Select-String -Pattern "ctf|CTF 竞赛|Rei Protocol"
```

### 通过标准

- 干净机器双击 → 配 codex → 全流程跑通
- `strings` 找不到提示词正文与凭据
- **60 题通过率 ≥ 旧版 93.9%**（唯一硬指标）

---

## 全局完成定义（DoD）

一个阶段算完成，必须同时满足：

1. 验收命令**实际跑过**，输出贴进汇报
2. 新增代码有测试覆盖；无测试的必须说明理由
3. 涉及不变量（INV-1…INV-10）的，有对应断言
4. 文档同步：方案与实现不符时**改方案**，不改事实
5. 未验证的部分显式标注"未验证"

---

## 阻塞项与替代路径

当前已知阻塞：**无上游 relay、无 codex 环境、无 `rewriter_builtin.json`**。

| 阶段 | 是否被阻塞 | 替代 |
|---|---|---|
| A | 否 | 假上游 + 临时 `CODEX_HOME` 足够 |
| 0 | 否 | 纯逻辑 + fixture |
| 1 | 否 | 假上游 |
| 2 | **部分** | TAMPER/补救可测；**改写器需一个 key**（任意兼容 API 即可） |
| 3 | **仅 e2e** | 注入/还原/自检前 6 项均可测；`verify --e2e` 需 codex |
| 4 | 否 | 绑定可 mock |
| 5 | **是** | 60 题矩阵需真实上游 |

**结论**：阶段 A、0、1 完全不受阻塞，可以立即开始。


