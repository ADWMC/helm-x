# helm-x-wails 实施规格

> **PLAN.md 定"为什么这样设计"，本文定"具体实现成什么"。**
> 三份文档的关系：
> - `PLAN.md` — 机制重写方案、不变量、架构、阶段
> - `PHASES.md` — 阶段任务清单与验收命令
> - **本文** — 数据模型、接口签名、事件契约、CLI、存储格式
> - `DESIGN.md` / `DESIGN-SPEC.md` — UI 契约与实现规格

---

## 1. Go 模块与目录

```
module github.com/ADWMC/helm-x

go 1.26
```

完整目录见 `PLAN.md` §3.2。本文只定义各包对外暴露的类型与函数。

**依赖方向（不可违反）**：

```
svc ──► proxy ──► protocol
 │        │
 │        └──► tamper / rewriter / codexcfg
 └──► selfcheck / watch / logging / assets

protocol 不 import 任何 internal 兄弟包
proxy 不 import svc
所有 internal 包不 import wails
```

---

## 2. 核心数据模型

### 2.1 请求视图（`protocol/responses`）

```go
// RequestView 是对上游请求体的只读投影。
// 关键：Body 始终保留原始字节；任何改写都基于 Body 做定点手术，
// 不经过反序列化-再序列化（INV-4）。
type RequestView struct {
    Body      []byte          // 原始字节，永不重新序列化
    Model     string
    Stream    bool
    Inputs    []InputItem     // 按原始顺序
    Tools     *jsonwalk.Value // 原始区间，不改写
    Reasoning *jsonwalk.Value
    MaxOut    *jsonwalk.Value
    Instructions *string      // 可能不存在
}

type InputItem struct {
    Role  string        // "user" / "assistant" / "system" / ""
    Kind  string        // "message" / "function_call" / "function_call_output" ...
    Texts []TextItem    // 该条目的文本内容，按顺序
    Value *jsonwalk.Value
}

type TextItem struct {
    Kind string // "input_text" / "output_text"
    Text string // 已反转义的明文
}
```

**约定**：
- `Parse` 失败 → 调用方**原样透传**，记一条 `Malformed` 事件，绝不猜（INV-9）
- `LastUserMessage()` 跳过含 `<environment_context>` 的条目（原版行为，`proxy.cpp:323`）
- `Rebuild` 只做三种手术：头部插入 system 条目、替换指定 message 文本、丢弃历史

### 2.2 响应状态（`proxy/response`）

```go
type Class uint8

const (
    ClassHealthy Class = iota
    ClassRefused
    ClassFlagged
    ClassUpstreamFailed
    ClassMalformed
    ClassUnresolved   // 三级补救均失败后的最终态
)

type ResponseState struct {
    Class    Class
    Status   int
    Headers  http.Header
    Body     []byte     // 非流式：完整；流式：仅已缓冲的窗口部分
    SSE      bool
    Complete bool       // 上游是否完整读完
    Text     string     // 用于判定的文本（JSON 字段 或 SSE 拼接结果）
    Reason   string     // 人读的原因，写进事件与日志
}
```

**判定顺序（严格，顺序即语义）**：

```
1. !Complete                       → UpstreamFailed
2. status ∉ 2xx                    → status ∈ 4xx/5xx 且命中 cyber 关键词 ?
                                        Flagged : UpstreamFailed
3. body 全空白                      → UpstreamFailed
4. body 不是合法 JSON/SSE           → Malformed
5. Text 命中拒绝规则                 → Refused
6. 其余                             → Healthy
```

**注意**：`Refused` 只在 2xx 上成立。`UpstreamFailed` 永不进 TAMPER。这是修 P1 的核心。
对应前端六态见 `DESIGN-SPEC.md` §4。

### 2.3 补救（`proxy/remedy`）

```go
type Action uint8

const (
    ActionNone Action = iota
    ActionRetry           // clean session（±改写）重发
    ActionAttachMarker    // 保留原话，附加标记
    ActionPassThrough     // 原样返回，记 unresolved
)

type Outcome struct {
    Action       Action
    FinalClass   Class
    Body         []byte
    UpstreamHits int   // 实际发出的上游请求数
    Notes        []string
}
```

**不变量 INV-8**：`ActionAttachMarker` 必须在保留原文的前提下附加。
禁止任何"用 marker 替换整段输出"的路径。

### 2.4 配置（`rewriter` / `codexcfg`）

```go
// 对应 %APPDATA%\helmx.config.json，字段名与旧版保持一致（兼容既有用户配置）
type Config struct {
    Rewriter struct {
        Enabled      bool   `json:"enabled"`
        Provider     string `json:"provider"`
        BaseURL      string `json:"base_url"`
        APIKey       string `json:"api_key"`
        Model        string `json:"model"`
        SystemPrompt string `json:"system_prompt,omitempty"`
        TimeoutSec   int    `json:"timeout_sec"`
        UseProxy     bool   `json:"use_proxy"`
        ProxyURL     string `json:"proxy_url"`
        Fallback     string `json:"fallback,omitempty"` // "none"(默认) | "local"
    } `json:"rewriter"`

    PromptMode string `json:"prompt_mode"`   // "default" | "v45" | "deepseek"

    UpstreamRetryEnabled      bool `json:"upstream_retry_enabled"`
    UpstreamMaxRetries        int  `json:"upstream_max_retries"`         // 0 = 无限
    UpstreamRetryDelaySeconds int  `json:"upstream_retry_delay_seconds"`

    // 新增（旧版读到会忽略）
    StreamWindowBytes int `json:"stream_window_bytes,omitempty"` // 默认 2048
    StreamWindowMs    int `json:"stream_window_ms,omitempty"`    // 默认 400

    // 已废弃：Context Gardener 相关字段。
    // 旧配置里可能存在，**读取时忽略、不报错、不再写回**，保证向后兼容。
    // 废弃原因：实测裁剪目标 type 在当前协议不存在，功能空转（FINDINGS-phase1 §5）。
    LegacyContextGardener json.RawMessage `json:"-"`
}
```

**热重载（INV-5）**：`config.Store` 持 `atomic.Pointer[Config]`；
每次请求先 `stat` 一次 mtime，变了才重载；UI 保存时主动 `Swap`。

---

## 3. 接口签名

### 3.1 `protocol/jsonwalk`

```go
type Kind uint8
const (KindObject Kind = iota; KindArray; KindString; KindNumber; KindBool; KindNull)

type Value struct {
    Kind       Kind
    Start, End int    // 原始字节区间（字符串含引号）
    Raw        []byte // 原切片，只读
}

func Parse(src []byte) (*Value, error)
func (v *Value) Member(key string) *Value           // 直接子成员，重复 key 取最后一个
func (v *Value) Index(i int) *Value                 // 数组元素
func (v *Value) Len() int
func (v *Value) String() (string, error)            // 反转义（支持 \uXXXX 代理对）
func (v *Value) Number() (string, error)            // 返回字面量，不转 float64
func (v *Value) InsertFirst(raw []byte) ([]byte, error)  // 数组/对象头部插入
func (v *Value) ReplaceString(val string) ([]byte, error)
func (v *Value) Splice(start, end int, raw []byte) ([]byte, error) // 通用区间替换
```

**要求**：保序、保转义风格、保数字字面量。任何 `error` → 调用方原样透传。

### 3.2 `proxy`

```go
type Engine struct{ /* ... */ }

type Options struct {
    Listen        string        // ":1800"
    Upstream      string        // relay base_url
    Passthrough   bool          // --passthrough，全部转换关闭
    DryRun        bool          // 只计算不发上游（N6）
    Retry         RetryOptions  // CLI 覆盖配置
    Events        EventSink     // 可 nil
}

func New(opts Options) (*Engine, error)
func (e *Engine) Start(ctx context.Context) error   // 阻塞
func (e *Engine) Stop() error
func (e *Engine) Status() Status

type Status struct {
    Running     bool
    Listen      string
    Upstream    string
    Sessions    int
    Requested   uint64
    ByClass     map[Class]uint64
}
```

### 3.3 `svc`（Wails 绑定层）

前端唯一入口。方法名即绑定名，返回 `(T, error)`。

```go
// --- 代理 ---
func (s *ProxyService) Status() (ProxyStatus, error)
func (s *ProxyService) Start() error
func (s *ProxyService) Stop() error
func (s *ProxyService) Restart() error
func (s *ProxyService) SetPassthrough(on bool) error

// --- codex 配置 ---
func (s *ConfigService) State() (ConfigState, error)
func (s *ConfigService) Apply() (ApplyResult, error)
func (s *ConfigService) Remove() (ApplyResult, error)
func (s *ConfigService) RestoreProxy() error
func (s *ConfigService) Preview() ([]ConfigDiff, error)   // 危险操作的"将写入什么"

// --- 提示词 ---
func (s *PromptService) Modes() ([]PromptMode, error)
func (s *PromptService) Current() (string, error)
func (s *PromptService) Set(mode string) error
func (s *PromptService) Preview(mode string) (string, error)

// --- 上下文 ---
func (s *ContextService) Get() (ContextSettings, error)
func (s *ContextService) Set(v ContextSettings) error

// --- 改写器 ---
func (s *RewriterService) Get() (RewriterView, error)      // api_key 必须脱敏
func (s *RewriterService) Set(v RewriterInput) error
func (s *RewriterService) Test(msg string) (string, error)

// --- 自检（异步）---
func (s *VerifyService) Run(e2e bool) (string, error)       // 返回 taskID，进度走事件
func (s *VerifyService) Cancel(taskID string) error
func (s *VerifyService) Last() (VerifyReport, error)

// --- 日志与请求 ---
func (s *LogService) Tail(source string, maxLines int) ([]LogLine, error)
func (s *LogService) Clear(source string) error
func (s *RequestService) Recent(limit int) ([]RequestRecord, error)

// --- 规则 / QA ---
func (s *RuleService) List() ([]Rule, error)
func (s *QAService) List() (QAView, error)
func (s *QAService) CheckUpdate() (QAView, error)

// --- 守护 ---
func (s *WatchService) Status() (WatchStatus, error)
func (s *WatchService) Start(intervalSec int) error
func (s *WatchService) Stop() error
```

---

## 4. 事件契约

事件名是接口的一部分，前端 `Events.On` 依赖它们。**改名前先读本节。**

| 事件 | 载荷 | 触发 | 替代原实现 |
|---|---|---|---|
| `proxy:log` | `LogLine` | 每次日志 | `/api/log` 轮询 |
| `proxy:cyber` | `CyberEvent` | cyber 判定 | `/api/cyber-log` 轮询 |
| `proxy:request` | `RequestRecord` | 每请求结束 | 无（新增 N4） |
| `proxy:status` | `ProxyStatus` | 启停 / 上游变更 | `/api/proxy` 轮询 |
| `verify:progress` | `VerifyProgress` | 自检每步 | `/api/zxwn` 轮询 |
| `verify:done` | `VerifyReport` | 自检结束 | 同上 |
| `watch:restore` | `WatchRestore` | 守护恢复成功 | `/api/watch` 轮询 |
| `config:changed` | `ConfigState` | 配置热重载 | 无 |

```go
type LogLine struct {
    TS    time.Time `json:"ts"`
    Level string    `json:"level"` // info | warn | error
    Text  string    `json:"text"`
}

type RequestRecord struct {
    ID         string    `json:"id"`
    TS         time.Time `json:"ts"`
    Method     string    `json:"method"`
    Path       string    `json:"path"`
    SessionID  string    `json:"sessionId,omitempty"`
    InBytes    int       `json:"inBytes"`
    OutBytes   int       `json:"outBytes"`
    Injected   bool      `json:"injected"`
    Class      string    `json:"class"`       // 与 useVerdictStyle 的 Verdict 对应
    Action     string    `json:"action"`      // none | retry | attach_marker | pass_through
    UpstreamHits int     `json:"upstreamHits"`
    DurationMs int64     `json:"durationMs"`
    Note       string    `json:"note,omitempty"`
}

type VerifyProgress struct {
    TaskID string `json:"taskId"`
    Step   int    `json:"step"`
    Total  int    `json:"total"`
    Name   string `json:"name"`
    OK     bool   `json:"ok"`
    Detail string `json:"detail"`
}
```

**约束**：`RequestRecord.Class` 的取值必须与前端 `Verdict` 类型逐一对应（`DESIGN-SPEC.md` §4）。
新增状态要同时改 Go 常量与 `useVerdictStyle.ts`。

---

## 5. CLI

入口是同一个 exe。无参数 → GUI。

```
helmx                          GUI（双击）
helmx proxy [options]          无窗口启动代理
helmx ui [--port 8090]         可选：降级浏览器控制台（WebView2 不可用时）
helmx apply                    注入 codex config
helmx remove                   还原并清理
helmx verify [--e2e]           自检
helmx activate                 发送激活词验证
helmx watch [interval]         自愈守护（前台）

proxy options:
  --listen PORT          默认 1800
  --upstream URL         缺省时从 codex config 自动读取
  --max-retries N        N=0 表示无限（与旧版一致）
  --retry-delay SECONDS  固定间隔，默认 3
  --no-retry
  --passthrough          关闭全部内容转换
  --dry-run              只计算，不发上游（N6）
  --restore              手动还原 codex config
```

**与旧版的兼容承诺**：上述 `proxy` 选项全部保留原有语义（`PLAN.md` §5.5）。
`--max-retries` 与 `--no-retry` 互斥时返回 1 并提示（旧版行为）。

---

## 6. 存储与文件

| 路径 | 内容 | 归属 |
|---|---|---|
| `~/.codex/config.toml` | 用户配置，注入 base_url | 用户，代理只改指定键 |
| `~/.codex/config.toml.helmx-bak` | 首次注入前备份 | 代理写，INV-6 |
| `~/.codex/config.toml.helmx-proxy-bak` | base_url 还原点 | 代理写 |
| `~/.codex/helmx.log` | 代理日志 | 代理写 |
| `~/.codex/helmx-cyber.log` | cyber 事件日志 | 代理写 |
| `%APPDATA%\helmx.config.json` | 全部用户设置 | 用户，UI 读写 |
| `%APPDATA%\helmx\asset.key` | 资源解密密钥 | 构建期读，**不入 git** |

**INV-6 写规则**：任何 config.toml 写入必须先过 TOML 校验、写临时文件、再原子替换。
替换前把当前非代理态内容刷新到还原点。

---

## 7. 可观测性格式

日志单行格式（与旧版 `log_info` 风格一致，便于对照）：

```
2026-08-14T14:32:07.123Z INFO  proxy: POST /v1/responses [INJECT] 12.4KB -> 14.1KB
2026-08-14T14:32:07.540Z INFO  proxy: upstream 200 (8.2KB) class=Healthy
2026-08-14T14:32:07.541Z INFO  proxy: done in 417ms action=none
```

cyber 事件额外记结构化字段：

```
2026-08-14T14:33:10.001Z INFO  cyber: status=403 triggers=inject,payload
                                    rewrite=ok session=clean result=rewritten_pass
```

**新增**：`class=` 与 `action=` 字段。这是 `PLAN.md` §7 新旧对照测试的唯一数据来源。

---

## 8. 错误处理约定

| 场景 | 行为 |
|---|---|
| 请求体无法解析 | 原样透传 + 记 warning（INV-9） |
| 配置读写失败 | 返回 error 给调用方，**不静默降级** |
| 上游连接失败 | 按重试策略；耗尽后 `UpstreamFailed` 保真转发 |
| 改写 API 失败 | 直接进补救第 2 步，**不重试**（INV-2） |
| 资源解密失败 | 启动即失败并明示，不带着空提示词裸跑 |
| codex home 找不到 | CLI 返回 1；GUI 显示空状态 + 下一步动作 |

**禁止**：把失败包装成成功。所有降级路径必须留下可见记录（`RequestRecord.Note` 或日志）。
