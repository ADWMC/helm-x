# helm-x-wails 重写方案 (v1)

> 用 **Go + Wails v3 + Vue 3** 重写 `helm-x`。
> 本文件从**机制**出发重新设计，不是把 C++ 文件逐一对映到 Go 文件。
> v0 的映射表已废止；本文档是唯一方案。

---

## 0. 先说重写要解决的问题

> **⚠️ 本节已按阶段 A 实测结果修订（见 [`FINDINGS.md`](FINDINGS.md)）。**
> P1、P4 两条原判定被**证伪**，已从问题清单移除或降级。
> 保留修订痕迹，避免再犯"用推断代替观察"的错。

读完 C++（~3100 行手写核心）后，**经实测确认**的问题按严重度排：

| # | 问题 | 证据 | 状态 | 本方案的解法 |
|---|---|---|---|---|
| **P2** | **伪造响应内容**：拒绝响应被整体替换为 marker，模型原话**完全删除**。客户端收到的是"据称已执行"的空壳，且状态码是 200 | 实测 A-2：原始字节 216B，含 marker，**不含**模型原话 | ✅ **已实测确认** | §5.5 分级补救 `retry → attach_marker → pass_through`；**INV-8 绝不删除模型原有输出** |
| **P3** | **SSE 全缓冲**：响应完整收完才转发，首字延迟 = 生成总时长 | 实测 A-3：`starttransfer=1.252s` ≈ `total=1.254s`（假上游 4 帧 × 300ms） | ✅ **已实测确认** | §5.5 首段缓冲窗口 + `Flush` 门控 |
| **P5** | **无单元测试**：0 个 C++ 测试 | `tests/` 目录清单 | ✅ 已确认 | §7 分层测试 |
| **P6** | **密钥在 git 里**：XOR 种子 `0x5A5A` 硬编码，注释自陈 "NOT cryptographic, just anti-glance" | `tools/embed.py:19`；`src/resources_generated.cpp` 882KB | ✅ 已确认 | §5.2 密钥外部化 |
| **P7** | **重写逻辑存在两套**：`rewrite_prompt.txt` 与 C++ 里的 `kRules` 不同步 | `rewrite.cpp:394-411` | ✅ 已确认 | §5.6 只保留 LLM 路径 |
| **P8** | **e2e 假阴性**：SSE 场景下 `is_refusal(resp_retry)` 扫整个响应，与 TAMPER 语义不一致 | `proxy.cpp:1194` vs `1182` | ⚠️ 推断 | §5.5 统一 `ResponseState` 判定 |
| **P9** | **配置靠字符串手术**：TOML 逐行替换，无结构模型 | `config.cpp:150-315` | ⚠️ 推断 | §5.3 行级模型 |
| **P10** | **UI 是 841 行单文件**，30 个 `/api/*` 手拼 JSON | `dashboard.html`、`ui.cpp:592-627` | ✅ 已确认 | §5.7 Wails 绑定 + 事件 |
| **P4'** | **裸字符串搜索的鲁棒性风险**：`body.find("\"role\":\"user\"")` 可能误伤正文里含该字面量的消息 | `proxy.cpp:291-440` | ⚠️ **inconclusive**：A-4 变更字段顺序仍正常，未构造出误伤用例 | §5.4 保序 JSON 扫描器（按**鲁棒性**改进，非按已知 bug） |

### 被证伪的两条（保留记录）

| 原判定 | 原依据 | 实测结论 |
|---|---|---|
| ~~**P1** 假通过：5xx 错误体被当成功转发~~ | `proxy.cpp:1107` 判 `2xx && body 非空` | **REFUTED**。A-1 实测：旧版正确透传 502，未改写。漏看了 `proxy.cpp:1093-1095` 的 `invalid_error_body` 分支 |
| ~~**P4** 字符串包含当协议解析 → 字段顺序变化即失效~~ | `proxy.cpp:291-440` | **REFUTED（表面）**。A-4 实测：字段顺序打乱后仍正常命中 |

**一句话**：v0 的方案是"搬房子"，把 C++ 的房间一比一盖到 Go 里。
v1 先定不变量，再从机制重推设计。
**阶段 A 的教训**：读完代码不等于验证过代码 —— 两条推断被自己的实测推翻。

---

## 1. 重写的不变量

重写允许改实现，以下**行为契约**必须逐条保真（每条都有测试锚点）：

| ID | 不变量 | 依据 | 锚点测试 |
|---|---|---|---|
| INV-1 | **绝不中断会话**。代理在成功路径上不得给 codex 制造原本不存在的错误 | 项目存在意义 | `TestNoSyntheticError` |
| INV-2 | **不消耗用户额度**。可选 LLM 阶段（改写）失败时，代理继续走确定性路径，不额外重试 | 成本 | `TestRewriteFailureDoesNotRetry` |
| INV-3 | **不覆盖上游状态码**。真拒绝保持拒绝，成功保持成功 | `proxy.cpp:1082-1087` | `TestStatusPassthrough` |
| INV-4 | **兼容 Responses API 的任意合法请求体**。不依赖 key 顺序、字段位置、缩进、转义风格 | `proxy.cpp:114-238` 的存在理由 | `TestJSONWalker`（转义/嵌套/重复 key） |
| INV-5 | **配置改动立即生效**，无需重启代理 | `proxy.cpp:884` 注释 | `TestLiveReload` |
| INV-6 | **用户文件可回滚**。任何 `config.toml` 改写必须原子，且备份足以还原到初始字节 | `config.cpp:95-120` | `TestByteRoundTrip` |
| INV-7 | **零外部依赖、单文件分发** | README / CONTRIBUTING | 打包冒烟 |

**新增不变量**（原版做不到的）：

| ID | 不变量 |
|---|---|
| INV-8 | **绝不删除模型的原有输出**。补救最差情况也必须把原话交给用户（§5.5）。**阶段 A 实测 A-2 确认旧版违反此条**：SSE 拒绝场景下模型原话被完全删除，返回 216B 的伪造响应 |
| INV-9 | **未知形态的请求体原样透传**，不猜测、不改写（§5.4） |
| INV-10 | **凭据不落仓库**（§5.2） |

---

## 2. 提取出的机制清单

从 C++ 里提取的、与"文件在哪"无关的功能单元。这是重写的真正清单。

| M | 机制 | 现存实现 | 状态 |
|---|---|---|---|
| M1 | 本地映射代理（127.0.0.1:1800 → 上游 relay） | `proxy.cpp:806-1246` | 保留，重实现 |
| M2 | 系统指令注入（插入 `input[0]`，不覆盖 `instructions`） | `proxy.cpp:515-550` | 保留，重实现 |
| M3 | 上游重试（固定间隔、0=无限、4xx 除 408/429 不重试） | `proxy.cpp:720-793` | 保留，策略可插拔 |
| M4 | ~~Context Gardener（按字节阈值裁 tool output，base64 无条件裁）~~ **❌ 已废弃，不重写** | `proxy.cpp:240-287` | **删除**：实测裁剪目标 `function_call_output` / `custom_tool_call_output` 在当前协议里**不存在**，旧版功能空转。该功能无实际作用，新版**不实现**。见 [`FINDINGS-phase1.md`](FINDINGS-phase1.md) §5 |
| M5 | cyber flag 判定（仅 4xx/5xx + 关键词） | `proxy.cpp:939-946` | 保留，收进状态机 |
| M6 | 改写器（LLM 改写 + 拒绝反馈 + 3 次换角度重试） | `rewrite.cpp:358-428` | 保留 LLM 路径，砍本地规则 |
| M7 | clean session 重建（只留 model/input/max_output_tokens/reasoning/tools） | `proxy.cpp:441-509` | 保留，改为节点手术 |
| M8 | TAMPER（26 条正则） | `tamper.cpp` + `tamper_rules.txt` | 保留规则，**改掉补救策略** |
| M9 | config.toml 注入（base_url 换成本地代理） | `config.cpp:395-450` | 保留 |
| M10 | config.toml 上下文默认值注入（3 个键） | `config.cpp:317-327` | 保留 |
| M11 | 自检 7 项 + e2e 激活 | `verify.cpp` | 保留，独立成 CLI 可跑 |
| M12 | 自愈守护（检测注入失效 → 恢复） | `watch.cpp` | 保留 |
| M13 | 日志 / cyber 日志 | `log.cpp` | 保留，改为结构化 + 事件 |
| M14 | QA 云更新（GitHub → 缓存 → 内置） | `ui.cpp` + `qa.json` | 保留 |
| M15 | CLI 子命令 | `main.cpp` | 保留 |
| M16 | 资源内嵌 | `resources.cpp` + `embed.py` | 保留机制，换实现 |

**原实现里没有、重写要补的**：

| N | 机制 | 为什么 |
|---|---|---|
| N1 | 分层传输模式（流式直通 / 缓冲补救） | 修 P3（**已实测确认**：旧版 `starttransfer` ≈ `total`，1.25s） |
| N2 | Responses 协议适配器（入站 / 出站分离） | 修 P4/P8 |
| N3 | 会话状态（按 thread/session id 绑定管线模式） | 原版每次请求从零推断，跨请求上下文丢失 |
| N4 | 可观测性（每个请求的结构化记录 + 事件） | 原版只能 `findstr` 日志 |
| N5 | 回滚安全性（备份链 + 恢复演练自检） | 原版 `remove_all` 直接覆盖，无冲突检测 |
| N6 | 干跑模式（`--dry-run`：全部计算，不发上游） | 调提示词/规则时不必真烧额度 |
| N7 | 设计系统（Tailwind 4 + daisyUI 5，语义色对齐判定状态） | 原版 841 行单文件手写 CSS，无法维护（§5.9） |

---

## 3. 架构

### 3.1 形态

```
┌──────────────────────────────────────────────────────────┐
│  helmx.exe  (单文件)                                       │
│                                                           │
│  ┌────────────┐   ┌─────────────────────────────────┐    │
│  │  Wails App │   │  Proxy Engine (net/http :1800)  │    │
│  │  桌面窗口   │   │  管线 · 状态机 · 上游客户端      │    │
│  │  Vue 3     │◄──┤  事件总线                        │    │
│  └────────────┘   └─────────────────────────────────┘    │
│        ▲                        ▲                         │
│        │ 绑定/事件               │ HTTP                    │
│  ┌─────┴────────────────────────────────────────────┐    │
│  │  服务层: proxy / config / rewriter / logs / qa    │    │
│  └──────────────────────────────────────────────────┘    │
│  ┌──────────────────────────────────────────────────┐    │
│  │  领域层: protocol · pipeline · tamper · rewriter  │    │
│  │          config · selfcheck · watch · logging      │    │
│  └──────────────────────────────────────────────────┘    │
│  ┌──────────────────────────────────────────────────┐    │
│  │  基础设施: assets · store · exec · version         │    │
│  └──────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────┘
       ▲                                        ▲
       │ codex CLI                              │ 外部 LLM API
       │ (base_url → :1800)                     │ (上游 relay + 改写器)
```

**关键决定：代理引擎不依赖 Wails**。GUI 只是它的一个观察者/控制者。因此 `helmx proxy` 可以无窗口运行，Wails 出问题也不影响核心。

### 3.2 目录（按职责，不按原文件）

```
helm-x-wails/
├─ main.go                          CLI 分发（无参 → GUI）
├─ internal/
│  ├─ protocol/                     ★ 新：协议层，零业务
│  │  ├─ jsonwalk/                  保序 JSON 扫描器（M4/M6/M7 的地基）
│  │  ├─ responses/                 Responses API 适配器（N2）
│  │  └─ sse/                       流式解析器
│  ├─ proxy/                        ★ M1-M8 的编排
│  │  ├─ engine.go                  生命周期（Start/Stop/Status）
│  │  ├─ request.go                 入站规范化
│  │  ├─ response.go                出站状态机（P1/P2/P8 的修复点）
│  │  ├─ remedy.go                  补救阶梯 retry/attach/marker
│  │  ├─ session.go                 clean session 重建（M7）
│  │  ├─ upstream.go                上游客户端 + 重试策略（M3）
│  │  └─ observe.go                 结构化记录 + 事件（N4）
│  ├─ tamper/                       M8（规则加载 + 分类，不含策略）
│  ├─ rewriter/                     M6（仅 LLM 路径）
│  ├─ codexcfg/                     M9/M10（注入 + 备份 + 还原）
│  │  ├─ doc.go                     行级 TOML 模型
│  │  ├─ locate.go                  定位 codex home
│  │  ├─ edit.go                    点修改：设置键 / 插入键
│  │  ├─ backup.go                  备份链 + 原子写 + 还原（N5）
│  │  └─ probe.go                   探测 codex home / provider / base_url
│  ├─ selfcheck/                    M11
│  ├─ watch/                        M12
│  ├─ logging/                      M13
│  ├─ assets/                       M16 + M14
│  └─ svc/                          Wails 服务层（前端唯一入口）
├─ frontend/                        Vue 3 + Tailwind 4 + daisyUI 5
│  ├─ src/style.css                 主题定义（@plugin daisyui，§5.9）
│  ├─ src/composables/              判定→样式映射等
│  └─ src/pages/                    10 个页面（原 9 个 + 请求页）
├─ assets/                          prompt / rules / qa / 内置配置
└─ docs/
```

**依赖方向**：`svc → proxy → protocol`；`protocol` 不 import 任何 `internal/` 兄弟包。`proxy` 不知道 Wails 存在。

---

## 4. 请求管线（重新设计）

### 4.1 现状管线的问题

原版是一条 400 行的 `handle_client` 线性过程（`proxy.cpp:806-1246`），所有分支写在一个函数里，状态靠局部变量互相覆盖（`status`/`resp_body`/`ok`/`cyber_flagged`/`tampered` 反复被赋值），这就是 P1/P8 的温床。

### 4.2 新管线

```
                     ┌─────────────────────────────┐
  codex ──HTTP──►    │  1. Normalize               │
                     │     解析请求 → RequestView   │
                     │     （识别不了 → 原样透传）    │
                     └──────────────┬──────────────┘
                                    ▼
                     ┌─────────────────────────────┐
                     │  2. Inject                  │
                     │     input[0] 插 system       │  ◄── 已实测有效（FINDINGS-phase1 §3）
                     └──────────────┬──────────────┘
                                    ▼
                     ┌─────────────────────────────┐
                     │  3. Send (with retry)       │  ◄── M3
                     └──────────────┬──────────────┘
                                    ▼
                     ┌─────────────────────────────┐
                     │  4. Interpret               │  ★ P8 修复点
                     │  上游响应 → ResponseState    │
                     └──────────────┬──────────────┘
                                    ▼
        ┌───────────────────────────┼───────────────────────────┐
        │                           │                           │
        ▼                           ▼                           ▼
   ┌─────────┐              ┌─────────────┐            ┌──────────────┐
   │ Healthy │              │  Refused    │            │ Flagged      │
   │ 直接转发 │              │  TAMPER     │            │ 改写+新会话   │
   └─────────┘              └──────┬──────┘            └──────┬───────┘
                                   │                          │
                                   └────────┬─────────────────┘
                                            ▼
                                  ┌───────────────────┐
                                   │  5. Remedy 阶梯    │  ★ P2 修复点
                                  │  retry → attach → │
                                  │  passthrough       │
                                  └─────────┬─────────┘
                                            ▼
                                  ┌───────────────────┐
                                   │  6. Transmit      │  ★ P3 修复点
                                  │  流式 or 整体      │
                                  └───────────────────┘
```

### 4.3 与旧版的行为差异（逐条可测）

> 「旧版」列已按阶段 A 实测校正。**~~删除线~~ 表示原推断被证伪。**

| 场景 | 旧版（实测） | 新版 |
|---|---|---|
| ~~上游 502 + JSON 错误体~~ | ~~判为 ok 转发~~ **实测：正确透传 502，未改写**（A-1）。此差异**不存在** | 保持同样正确，状态机只是让它有单一判定入口 |
| 上游 200 + 拒绝文本（非流式） | 附加 marker，**原话保留**（A-4） | 同（这是旧版正确的地方） |
| 上游 200 **SSE** + 拒绝文本 | **整体替换为 marker JSON，原话完全丢失**（A-2，216B 响应，不含模型原话） | 先 clean-session retry；仍拒绝 → 流式追加 marker（**原话保留**，INV-8） |
| 上游 200 SSE + 正常内容 | **全缓冲**：`starttransfer` ≈ `total`（A-3，1.25s） | 真流式，首字节即转发（目标 <100ms） |
| 上游 4xx + cyber flag | 改写 + 新会话 + 重发 | 同，但状态机保证不会把结果和错误混淆 |
| 请求体无法解析 | 裸搜索在**已测场景**下正常（A-4）；误伤风险未证实 | 原样透传 + 记一条 warning（按鲁棒性改进，非修已知 bug） |

---

## 5. 关键机制设计

### 5.1 保序 JSON 扫描器（`protocol/jsonwalk`）

一切请求体手术的地基。**不做反序列化**，只在原始字节上定位与替换。

```go
// 概念签名
type Value struct {
    Kind  Kind      // Object / Array / String / Number / Bool / Null
    Start, End int  // 原始字节区间，含引号
    Raw   []byte    // 原始切片，绝不重新序列化
}

func Parse(src []byte) (*Value, error)              // 只读遍历，保序
func (v *Value) Member(key string) *Value           // 直接子成员
func (v *Value) InsertFirst(raw []byte) ([]byte, error)  // 往数组/对象头部插
func (v *Value) ReplaceString(val string) ([]byte, error) // 替换字符串值（自动转义）
```

**要求**：
- 保序、保原始转义风格、保数字字面量（`12345678901234567890` 不经过 float64）
- 支持 `\uXXXX` 代理对、重复 key（取最后一个，与 JSON 语义一致）
- 越界/非法输入返回 error，调用方一律**原样透传**（INV-9）

**正确性锚点**：用真实 codex 请求体做 fixture，对每个被修改的请求断言"除目标字段外逐字节相同"。

### 5.2 资源与凭据（`assets`）

**问题**：`embed.py` 的 XOR 种子在仓库里（P6）。而 `assets/rewriter_builtin.json` 是内置免费改写凭证，**不在 git**（`.gitignore`）——所以"加密资源"这层实际保护的正是这份凭证，而密钥又公开在 `embed.py` 里，等于零保护。

**方案**：密钥从环境/文件读取，且**构建失败优于静默降级**。

```
HELMX_ASSET_KEY          (env)
  ↓ 缺失时
%APPDATA%\helmx\asset.key  (文件，不入 git)
  ↓ 缺失时
构建失败（除非显式 -tags plainembed 走明文内嵌，用于本地开发）
```

- 算法：AES-GCM（Go 标准库），替换 XOR + PRNG 密钥流。
- 明文资产仍在 `assets/`（与原仓库一致），加密在构建期由 `go generate` 完成，产物 `assets_encrypted.go` 不入 git。
- **不宣称这是安全边界**——本地工具防不住逆向。目标只是：`strings helmx.exe` 看不到提示词和凭据、密钥不再随仓库分发。

### 5.3 config.toml 处理（`codexcfg`）

原版是逐行字符串替换（`config.cpp:150-315`），几乎不可测。改为**行级文档模型**：

```go
type Doc struct {
    lines []Line        // 原始行，含换行符
    // Line 记录原始字节 + 表上下文（哪个 [section]）
}
func Parse(raw []byte) *Doc
func (d *Doc) GetTopLevelString(key string) (string, bool)
func (d *Doc) GetInTable(table, key string) (string, bool)
func (d *Doc) SetTopLevelString(key, val string) bool   // 只改目标行的值区间
func (d *Doc) InsertIfAbsentTopLevel(key, val string)   // 按现有 EOL 风格插到头部
func (d *Doc) Bytes() []byte                             // 未改动的行逐字节返回
```

**为什么不是 `BurntSushi/toml`**：它解决了 P9 的一半（解析正确），但**写回会重排格式**，会破坏用户配置的注释和布局（INV-6 要求可回滚且最小 diff）。行级模型两头都满足。

**可测性**：表驱动 — 给一段真实 config.toml → 断言输出只有目标行变化，其余 `bytes.Equal`。

### 5.4 协议适配器（`protocol/responses`）

把"哪些字段是用户消息""哪个是 model""哪个是 tools"从业务里剥离。

```go
type Adapter interface {
    Name() string
    Parse(body []byte) (*RequestView, error)
    LastUserMessage(v *RequestView) (string, bool)
    Rebuild(body []byte, spec RebuildSpec) ([]byte, error)
}

type RequestView struct {
    Model     string
    Stream    bool
    Inputs    []InputItem     // 按原始顺序
    Tools     *jsonwalk.Value
    Reasoning *jsonwalk.Value
    MaxOut    *jsonwalk.Value
    // 原始 body 始终保留，任何 Rebuild 都基于它做定点手术
}

type RebuildSpec struct {                 // clean session 用
    KeepSystemPrompt bool
    ReplaceMessage   *string
    StripHistory     bool
}
```

**入站/出站分离**：`Adapter` 只负责请求；响应由 `ResponseState`（§5.5）建模。原版的 P8 就来自两者共用一个字符串扫描。

**可扩展**：上游若换成 chat_completions 风格，新增一个 `Adapter` 实现即可。原版的注入逻辑注释里提到"也兼容 chat messages 格式"（`proxy.cpp:512`），但实际只实现了 Responses——这个缺口由接口设计补上。

### 5.5 ★ 响应状态机与补救阶梯（P1/P2/P3/P8 的修复核心）

```go
type ResponseState struct {
    Class    Class     // Healthy | Refused | Flagged | UpstreamFailed | Malformed
    Text     string    // 用于判定/改写的文本（JSON 字段 或 SSE 拼接）
    SSE      bool
    Status   int
    Complete bool      // 上游是否完整读完
}

func Classify(status int, headers http.Header, body []byte) ResponseState
```

**判定规则（严格）**：

| Class | 条件 |
|---|---|
| `UpstreamFailed` | `!Complete`（连接中断/超时）或 `status ∉ 2xx` 或 body 全空白 |
| `Flagged` | `status ∈ 4xx/5xx` **且** body 命中 cyber 关键词（M5，保持原语义） |
| `Refused` | `status ∈ 2xx` **且** `Complete` **且** 文本命中拒绝规则 |
| `Malformed` | `status ∈ 2xx` 但 body 不是合法 JSON/SSE |
| `Healthy` | 其余 |

> 关键：`Refused` 只可能在 2xx 上成立。`UpstreamFailed` 永远不进 TAMPER。
>
> **修订说明**：原写"这一条直接消灭 P1"。阶段 A 实测**证伪了 P1** ——
> 旧版本来就正确透传 5xx（`proxy.cpp:1093-1095` 的 `invalid_error_body` 分支）。
> 状态机的真实价值在于：**统一判定入口**、消除 P8 的语义不一致、
> 以及为 **INV-8**（绝不删除模型原有输出）提供单一执行点。不是因为旧版会把 5xx 当成功。

**补救阶梯（`remedy`）**：

```
Refused / Flagged
   │
   ├─ 1. Retry          : clean session（±改写消息）重发一次
   │     成功 → 返回新响应
   │     失败 ↓
   ├─ 2. AttachMarker   : 保留模型原话，在其前/后附加标记
   │                     2xx JSON → 替换 output_text 字段值 = marker + 原话
   │                     2xx SSE  → 直接在流尾追加 marker 事件（原话已流过）
   │     不可行 ↓
   └─ 3. PassThrough    : 原样返回 + 记一条 unresolved 事件，让用户看见
```

**相对旧版的三处改动**：
- 旧版第 2 步是 **Replace**（`proxy.cpp:1207` 整个 body 换成 marker，原话丢失）→ 新版是 **Attach**（INV-8）。
- 旧版 SSE 分支只能 Replace（因为全缓冲时流还没发出去）→ 新版流式模式下原话已转发，只需追加。
- 旧版 `Marker` 是硬编码常量且写死两种形态 → 新版是模板 + 形态适配。

**流式门控（P3）**：`stream=true` 时按 SSE 事件粒度转发。为了让"Flush 前尚可补救"，引入**首段缓冲窗口**：

```
上游 SSE ──► [ 首段缓冲: 累积前 N 字节 或 T 毫秒 ]
                    │
                    ├─ 窗口内检出 Refused → 丢弃缓冲，走 Retry（用户什么都看不到）
                    └─ 窗口结束 → Flush 缓冲，之后纯转发（O(1) 内存）
                                        │
                                        └─ 窗口后检出 Refused → 追加 marker（原话已在客户端）
```

`N`（默认 2KB）、`T`（默认 400ms）可配。这样常见的"一开头就拒绝"能在零副作用下走 Retry，而长回答不再被全缓冲。**这是原版做不到的**：原版要么全缓冲（慢），要么无法补救。

**内存**：非流式完整读（上限 16MiB，与原版一致，`proxy.cpp:706`）；流式只留窗口，其余边读边写。

### 5.6 改写器（`rewriter`）

- **只保留 LLM 路径**（P7）。本地 `kRules` 从主路径移除；如果确实需要确定性降级，做成显式配置 `rewriter.fallback: "none" | "local"`，默认 `none`。
- API 失败 → 直接进入补救阶梯第 2 步，**不再重试**（INV-2）。
- 保留原版的 3 次"换角度"重试（`rewrite.cpp:368-388`），但三次都失败后不再回落本地规则，而是标记 `rewrite_failed` 并继续。
- 系统提示词、禁用词、示例全部从 `assets/rewrite_prompt.txt` 加载（保持可热更新），不在 Go 代码里重复一份。
- 拒绝反馈（`refusal_text`）与对话上下文照原样传给改写模型（`rewrite.cpp:438-449`）。

### 5.7 GUI（`svc` + `frontend`）

对齐原 9 个页面，但去掉轮询、去掉手拼 JSON：

| 页面 | 后端 | 原实现 |
|---|---|---|
| 总览 | `Overview.Status()` + `proxy:request` 事件 | `/api/status` 轮询 |
| 验证 | `Verify.Run(e2e)` + `verify:progress` 事件 | `/api/zxwn` 轮询 |
| 服务 | `Proxy.Start/Stop/Restart`、`Watch.Start/Stop` | 多个 POST |
| 提示词 | `Prompt.Mode()/SetMode()` | `/api/prompt-mode` |
| 上下文 | `Guard.Get()/Set()` | `/api/context` |
| QA | `QA.List()/CheckUpdate()` | `/api/qa` |
| 改写器 | `Rewriter.Get/Set/Test()` | `/api/rewriter*` |
| 日志 | `Logs.Tail()` + `proxy:log` 事件 | `/api/log` 轮询 |

**新增页面（N4）**：`请求` — 每个请求一行（时间 / 路径 / 入出字节 / 注入 / 判定 / 补救动作 / 耗时）。这是 P1/P2/P8 这类问题唯一的可视证据来源。

**托盘**：关窗口 → 托盘（可配直接退出）；托盘菜单：显示控制台 / 暂停代理 / 恢复配置并退出。退出走 `OnShutdown` → 还原 `config.toml`。单实例：第二次启动拉起已有窗口。

### 5.9 UI 组件库：daisyUI 5（Tailwind 4）

**决定**：前端用 **Tailwind CSS 4 + daisyUI 5.7.47**（MIT）作为组件库。已在本机核对：`daisyui@5.7.47`、`tailwindcss@4.3.3`、`@tailwindcss/vite@4.3.3`。

**为什么是它（针对本项目，不是泛泛的优点）**：

| 需求 | daisyUI 提供的 | 原 dashboard.html 的做法 |
|---|---|---|
| 状态类语义色 | `success` / `warning` / `error` / `info` 直接对应**请求判定**（Healthy/Refused/Flagged/Failed） | 手写 CSS class + 硬编码十六进制 |
| 日志/事件流 | `chat`（多种气泡样式） | 手写 `div` + 等宽字体 |
| 请求表格 | `table`（含 `table-zebra`） | 手写 `<table>` 样式 |
| 关键指标 | `stat` / `stats`（标题+值+描述三件套） | 手写卡片 |
| 规则 / 提示词列表 | `list`、`collapse`（可折叠） | 手写 |
| 启停/开关 | `toggle`、`button`、`badge`、`loading`、`progress` | 手写 |
| 弹窗 / 二次确认（remove 还原配置这种危险操作） | `modal` | 手写或没有 |
| 提示 | `tooltip`、`toast`、`alert` | 手写 |
| 多主题 | 35 个内置主题 + 主题控制器 | 只有一套深色 |

**接入方式**（Tailwind 4 用 CSS 优先配置，不写 `tailwind.config.js`）：

```css
/* frontend/src/style.css */
@import "tailwindcss";

@plugin "daisyui" {
  themes: dark --default, light;   /* 默认深色，贴合原 dashboard 风格 */
}

/* 项目语义色：把「请求判定」直接映射到 daisyUI 主题变量 */
@plugin "daisyui/theme" {
  name: "helmx";
  default: true;
  color-scheme: dark;
  --color-primary: oklch(58% 0.233 277.117);
  /* base / neutral 等沿用 dark 主题，只覆盖强调色 */
}
```

```ts
// frontend/vite.config.ts
import tailwindcss from '@tailwindcss/vite'
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  // Wails 需要固定 base，避免资源路径问题
  base: './',
})
```

**按需打包**：daisyUI 5 支持 `@plugin "daisyui" { include: ...; }`，本项目只用到约 25 个组件，可在构建时只保留实际用到的，控制 CSS 体积（对桌面应用不是硬约束，但快照体积小没坏处）。

> **本文只定"用什么库"。视觉语言、组件边界、交互规则、文案规则见 [`DESIGN.md`](DESIGN.md)。**
> 组件库是手段；设计规则是契约。改界面行为前先读 DESIGN.md。

**约定（写进 CONTRIBUTING 性质的规则）**：
- 业务组件**只用 daisyUI 语义 class**（`btn btn-primary`、`badge badge-error`），不写裸 Tailwind 颜色值（`bg-red-500`）——避免主题切换失效。
- 语义映射集中在一处：`composables/useVerdictStyle.ts`，把 `Class`（§5.5）映射成 `badge-{success|warning|error|info}`。前端不自己判断颜色。
- 图表（请求量 / 拒绝率趋势）daisyUI **不提供**，自绘 SVG 或后续单独评估，不要顺手引一个大依赖进来。

**替代方案与代价**（诚实记录）：

| 方案 | 为什么没选 |
|---|---|
| 手写 CSS + 变量 | 原 dashboard 就是这么做的，841 行里一大半是重复样板；重写的目的之一就是消掉它 |
| Element Plus / Naive UI | 组件更全，但视觉是"企业后台"风，和原 dashboard 的紧凑信息密度差得远；且体积大得多 |
| Nuxt UI / shadcn-vue | 依赖 Tailwind 版本更紧、需要复制组件源码进入仓库维护 |
| **daisyUI** | 纯 CSS 插件、无 JS 运行时依赖、主题系统开箱即用、35 个主题可让用户自选深/浅 |

**风险**：daisyUI 5 要求 Tailwind 4（v4 写法与 v3 差异大，网上 v3 教程不能直接抄）。锁 `tailwindcss@4.x` + `daisyui@5.x`，并在 `package.json` 用 `~` 固定次版本。

### 5.8 会话状态（N3）

原版每个请求独立推断，且每请求重新读配置。新版：

```go
type Session struct {
    ID        string     // 从 session-id / thread-id 头提取
    Mode      Mode       // Streaming | Buffered
    Window    WindowCfg  // 首段缓冲参数
    Counters  Counters
    LastSeen  time.Time
}
```

- 键取上游请求头里的 `session-id` / `thread-id`（原版已经在转发这些头，`proxy.cpp:845-854`）。
- 有界 LRU（默认 256 会话 / 30 分钟 TTL），避免内存泄漏。
- 用途：同一会话内保持模式一致、统计拒绝率、给 UI 展示"这个会话触发了几次补救"。
- **不用于**改变上游请求内容——它是只读观察 + 策略选择。

---

## 6. 阶段划分（按机制，不按文件）

每个阶段独立可运行、可用 CLI 验证，不依赖 GUI。

### 阶段 0 — 地基（1 天）
`protocol/jsonwalk` + `protocol/sse` + fixture 收集（真实 codex 请求/响应抓包）
**验收**：`go test ./internal/protocol/...` 全绿；JSON 保真测试（改一个字段，其余逐字节相同）。

### 阶段 1 — 管线骨架 + 状态机（2 天）
`proxy/engine` + `request` + `response` + `upstream` + `observe`，**不含** 注入/改写/TAMPER
**验收**：假上游 + 真 codex，正常请求全透传；各 `Class` 判定有表驱动测试；流式首字节延迟 < 100ms。

### 阶段 2 — 三段补救（2 天）
`tamper` + `rewriter` + `remedy` + `session`
**验收**：`TestAgainstOldVersion` — 同一请求分别打旧版和新版，比对**判定结果**与**最终响应形态**，差异逐条记录（P2/P3 的修复点应表现为预期差异）。

### 阶段 3 — 配置与自检（1.5 天）
`codexcfg` + `selfcheck` + `watch` + CLI（apply/remove/verify/activate/watch/proxy）
**验收**：`helmx apply` → `helmx verify` 全 PASS；`remove` 后 `config.toml` 与备份**逐字节一致**；TOML 行级测试全绿。

### 阶段 4 — GUI（2.5 天）
`svc` 全部绑定 + 事件；Vue 10 页面 + 请求页；托盘 / 单实例 / 退出还原

**先做骨架再铺页面**（顺序见 `DESIGN.md` §9）：
`style.css` 主题 → `useVerdictStyle` 色映射 → `AppShell` → 4 个基础组件 → 然后 10 个页面。
跳过这步，10 个页面会长成 10 种样子。

**验收**：逐页对照旧 `:8090` 控制台，功能无缺失；对照 `DESIGN.md` 逐条核规则。

### 阶段 5 — 发布（1 天）
资源加密管线、图标、`wails3 package`、迁移说明（旧版配置 / 回滚步骤）
**验收**：干净机器双击 → 配 codex → 跑通；`strings helmx.exe` 找不到提示词与凭据。

**合计约 10 个工作日**（不含真机 e2e 联调返工）。

---

## 7. 测试策略

**分层，全部 `go test`，不需要 Python**：

| 层 | 方式 | 覆盖的不变量 |
|---|---|---|
| `jsonwalk` | 表驱动 + 真实 fixture + fuzz（`go test -fuzz`） | INV-4 / INV-9 |
| `sse` | 分块投喂，含跨 chunk 截断、`\r\n`/`\n` 混用 | INV-4 |
| `codexcfg` | `t.TempDir()` 真文件，注入/还原/损坏 TOML 拒绝写 | INV-6 |
| `response.Classify` | 表驱动：状态码 × body 形态 × 完整性 | P1 / P8 |
| `remedy` | 假上游脚本化（拒绝→拒绝→通过 等序列） | INV-1 / INV-8 |
| 管线整体 | `httptest` 假上游 + 真实请求体 | INV-1/2/3 |
| 流式 | `httptest` 慢速 SSE，断言首字节时间 | P3 |
| e2e | `go test -tags e2e`，真调 codex，默认跳过 | INV-1 |
| 前端 | Vitest + mock 绑定 | — |
| 前端样式 | 断言判定→class 映射（`useVerdictStyle`）不回归 | §5.9 语义色约定 |

**回归基线**：旧仓库 `tests/v005-100-test.py`（60 题矩阵，旧版 93.9%）改为打新代理重跑，**通过率不得低于旧版**。这是重写是否等价的唯一硬指标。

**新旧对照测试**（阶段 2 的核心工具）：同一批请求分别打旧版 `:1800` 和新版 `:1801`，逐条比对：
- 判定结果（Healthy/Refused/Flagged/Failed）
- 最终响应形态（原样 / 附加 marker / clean-session 结果）
- 上游请求体（注入后应逐字节相同）

差异分三类：`expected`（P1/P2/P3/P8 的修复）、`regression`（必须修）、`noise`（记录）。这份报告本身就是交付物之一。

---

## 8. 风险

| 风险 | 影响 | 对策 |
|---|---|---|
| **首段缓冲窗口选错** | 短拒绝流被漏过，或延迟变差 | 窗口参数可配 + 请求页可视化实际触发率；默认值保守（2KB/400ms） |
| **Wails v3 beta API 变动** | 升级编译失败 | Wails 只在 `svc/` + `main.go` 出现；领域层零依赖 |
| **上游 API 形态变化**（responses → 其他） | 注入/手术失效 | `Adapter` 接口；未知形态透传（INV-9）而不是猜 |
| **改写质量下降**（砍掉本地规则） | 无 key 用户失去改写 | 保留 `fallback: local` 开关；默认行为与旧版"有 key 时"完全一致 |
| **新旧对照测试成本高** | 阶段 2 拖期 | 只对照**判定与响应形态**，不做全量文本 diff |
| **WebView2 缺失** | 老系统起不来 | 降级到 `helmx proxy` 无窗口模式 + 可选本地控制台端口 |
| **daisyUI 5 只配 Tailwind 4** | 抄 v3 教程会写错（`tailwind.config.js` 已废弃） | 锁 `tailwindcss@~4.3` + `daisyui@~5.7`；配置全在 CSS（§5.9） |
| **前端组件各写各的** | 5 个页面之后风格发散 | 阶段 4 先定 4 个基础组件 + 1 处色映射，页面只组合 |
| **60 题基线波动是模型问题不是代码问题** | 误判回归 | 基线只用于**同版本对比**；单次波动不触发回滚，连续两次才告警 |

---

## 9. 待确认

1. **首段缓冲窗口**：默认 2KB / 400ms，是否够？（§5.5）
2. **本地改写规则**：默认关闭（只走 LLM），还是保留为显式 fallback？（§5.6，倾向默认关闭）
3. **CLI 保留范围**：全部 7 个子命令都留，还是只留 `proxy` / `apply` / `remove` / `verify`？
4. **版本号**：原仓库最新 release 是 `v0.0.8`。新版是从 `v0.1.0` 开始，还是另起 `v2.0.0` 表明是重写？
5. **`rewriter_builtin.json`**：内置免费改写凭证不在 git 里。你手上有吗？没有的话阶段 2 只能用自备 key 验证改写分支。
6. **旧仓库资产**：`assets/` 里的 5 个提示词变体（ctf-scoring / ctf-fusion / ctf-lite / sandbox-executor / deepseek 优化版）全部保留，还是本次只保留实际在用的？
7. ~~**主题**：daisyUI 默认给 `dark`（贴合原控制台），是否要开放"浅色 + 主题自选"给用户？~~
   **已定**：锁定深色单一主题，不做主题选择器（`DESIGN.md` §5.3）。
8. **文案语言**：界面文案中文（原 dashboard 是中文），还是中英双语？这会影响 `DESIGN.md` §4.2 术语表的落地方式。

---

## 相关文档

| 文档 | 内容 |
|---|---|
| **PLAN.md**（本文） | 机制重写方案：问题、不变量、架构、阶段、测试 |
| [`DESIGN.md`](DESIGN.md) | UI 设计语言：视觉层级、组件边界、交互规则、文案规则、主题色板 |

---

## 附：废弃的 v0 假设

| v0 的说法 | 为什么错 |
|---|---|
| "按 1:1 文件映射重写" | 会把 P1/P2/P3 的结构缺陷一起搬过来 |
| "手写 JSON 扫描器只是风险，尽量保真" | 它本身就是 P4 的根因；应是**重新设计**，不是保真 |
| "TAMPER 只是替换，行为要一致" | 旧版的 Replace 语义是缺陷（P2），一致地错没有意义 |
| "SSE 全缓冲保持等价即可" | P3 是可修的性能缺陷，等价不是目标 |
| "内置改写 API 缺失 = 可选功能" | 它是默认路径；缺失会导致阶段 2 无法验证 |

