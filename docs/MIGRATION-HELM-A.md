# 迁移方案：helm-a 能力并入 helm-x（保留外部 codex，去掉隔离运行时）+ UI 重设计

> 状态：**方案稿（未实施）**。2026-11-22。
> 目标仓：`helm-x-wails`（本仓库）。来源仓：`C:\Users\Administrator\Documents\GitHub\helm-a`（迭代版）。
> 约束：**只搬 helm-a 的功能，不搬它的 codex 隔离运行时**；界面按两份设计调研重做（现行深色被判定"看不清"，已实测）。
> 本文只定方案与验收；未获批准不动代码。

---

## 0. 三句结论

1. **隔离是 helm-a 全部功能的锚点，也是它最好删的部分**：`codexhome` / `settings` / `capture` / `isocodex` 的每个路径都写死 `%APPDATA%\helma\codex-home`，用户自己的 `~/.codex`（登录态、会话、MCP、profile）完全不被使用。删掉隔离 = 换个 home 根目录 + 去掉下载器状态机，功能本身一行都不用扔。
2. **值得搬的是五件真东西**：供应商/上游热切换（含 DPAPI 凭据）、请求层代号表与全文编码、调试报文捕获树、**提示词资产（竞赛主臂 + 路由规则集，按用户要求只迁主臂）**、报文与请求记录落盘。主臂已上线为 `ctf-ac`。
3. **界面问题不是"字太暗"，是"边界没了"**：实测现行深色主题正文 16.51:1（远超 AA），但控件边框只有 **1.13:1**、`/30` 文字 **2.27:1**、`/45` 文字 **3.83:1**（`python tools/contrast.py`）。重设计的核心是**浅色为默认 + 边界与层次重建**，不是把字调亮。

---

## 1. 为什么隔离不能要（证据）

| 依据 | 位置 | 含义 |
|---|---|---|
| 所有 home 路径写死 | `helm-a/main.go:33-37`、`internal/codexhome/codexhome.go:20`、`internal/isocodex/isocodex.go:63-64` | 一切读写落在 `%APPDATA%\helma\{bin,codex-home,capture}` |
| 隔离靠环境变量而非参数 | `internal/isocodex/launcher.go:120` | `CODEX_HOME=<isolated>`；0.167 无 `--codex-home`，用户无法"顺手用一次" |
| 启动链是一整套状态机 | `launcher.go`(143) + `manage.go`(159) + `pinned` 常量 | 下载 352MB → SHA-256 校验 → 登录向导 → 拉起终端 → 监控态；每一步都有独立失败面 |
| 用户资产被绕开 | `settings.json` 在 `%APPDATA%\helma` | 已有 codex 登录、历史会话、插件、profile 全部要重新配 |

**代价 vs 收益**：隔离换来的是"不碰用户 `~/.codex`"；付出的是每次新机重新登录、会话断代、352MB 下载、pinned 版本追更、终端拉起失败面。而**代理层的能力（注入、判定、补救、改写）与隔离完全无关** —— 只要 codex 打到 `127.0.0.1:1800`，注入就成立。helm-x 现有路径（定点改 `config.toml` 的 provider + 备份/逐字节还原）已经满足这个前提。

**迁移决定**：保留外部 codex 模式；把 helm-a 各包改成"根目录可注入"（`codexcfg.Home` 已有的形态），默认指向 `os.Getenv("CODEX_HOME")` 或 `~/.codex`。

---

## 2. 能力对照与迁移决策

| # | 能力 | helm-a 位置 | helm-x 现状 | 决策 |
|---|---|---|---|---|
| F1 | **供应商/上游热切换** | `internal/settings`(498) `Provider/ActiveProviderID`、`codexhome/providers_edit.go`、`proxy` route 快照 | 无（上游取自 config.toml，改要重启 codex） | **迁移（核心）** |
| F2 | **DPAPI 凭据** | `internal/secure`(4 文件)、`settings.KeyEnc` | 无（key 在 codex config 明文） | **迁移** |
| F3 | **代号表 codebook** ✅ | `internal/inject/codebook.go`(203) | 无 | **迁移**，默认关 |
| F4 | **全文编码 encode(mojibake)** ✅ | `inject`（`EncodeEnabled`）、`CODEBOOK-DESIGN §11.5` | 无 | **迁移**，默认关，与 F3 互斥 |
| F5 | **instructions 位注入 insert/append/replace-base** ✅ | `protocol/responses.AppendInstructionsTail`、`settings.Deploy` | 只在 `input[]` 插 system 消息（`request.go:191-207`） | **迁移**（新增部署语义） |
| F6 | **调试报文捕获** ✅ | `internal/obs`(269) `Capture` + `Put(reqID,name,data)` + `index.jsonl` + 配额/脱敏 | 无 | **迁移** |
| F7 | **请求记录持久化** ✅ | `proxy/reqlog.go`(264)、`obs.Store` 环形 200 | 内存 500 条，无落盘 | **迁移** |
| F9 | **会话浏览/删除** | `codexhome/state.go`(86) + `sessions/` | 无 | **迁移**（只读 + 备份后删） |
| F10 | **协议增强** ✅：responses 视图、SSE 私有事件（`rate_limits`/`response.metadata`） | `protocol/responses`(279)、`sse`(299) | 自有 `request.go`/`sse`，无私有事件解析 | **部分迁移**（视图取长，事件补充） |
| F11 | **出站代理探测 netenv** ✅ | `internal/netenv`(111)：env → 注册表系统代理 → 直连 | 用默认 transport（系统代理非 env 时连不上） | **迁移**（真机已验证踩坑） |
| F12 | **矩阵跑批 harness** | `proxy/prompt_matrix_test.go`(399)、`prompts/arms`、`ROUTING-RULES-v3.md` | `tests/helm-x-test-suite.py`（题库跑批，走 HTTP） | **迁移 HTTP 模式**；codex 模式改外部 home |
| F13 | **提示词资产**（主臂/规则集/题库） | `prompts/arms/g-composed-arm.md`(28.4KB, replace-base)、`prompts/ROUTING-RULES-v3.md` v4、`prompts/questions/questions.txt`(C01–C21)、`INVENTORY.md` | 6 个内置模式（ctf→ctf-v6） | **迁移**：主臂已上线为 `ctf-ac`（需 replace-base，已落地）；A1/A2 按用户要求不迁 |
| F14 | 判定六分类 + 补救阶梯 | `internal/refusal`(131) | `response.go`/`remedy.go`(330) 更完整（含 CyberPolicy 终局、JSON/SSE 附加） | **不迁移**（保留现有） |
| F15 | 隔离运行时（下载器/登录/终端拉起） | `internal/isocodex`(694) | 无 | **不做** |
| F16 | 启动参数（workDir / sandbox / approval / `Exec`） | `settings.WorkDir/Launch*`、`RuntimeService.Exec` | 无 | **不做**（属隔离启动链） |
| F17 | 一键启动按钮 + 阶段事件 | `RuntimeService.Stage`、`Pipeline.vue` | 无 | **不做**；概览改为"代理状态 + 上游供应商" |

失败面净减少：删 694 行 `isocodex` + 一条 352MB 下载链 + 一个 pinned 版本追更义务。

---

## 3. 目标架构

```
codex（用户自己的 ~/.codex，登录态与会话原样保留）
  └─ config.toml:  model_provider = "helmx-local"
                   [model_providers.helmx-local] base_url = "http://127.0.0.1:1800/v1"
        │
        ▼
  127.0.0.1:1800  internal/proxy
    ├─ route 快照（atomic.Pointer）      ← 供应商 / key / model / headers 热切换
    ├─ inject：instructions(insert|replace-base) + codebook + encode
    ├─ capture：00–54 报文树（可选）
    ├─ 转发（netenv 决定是否走系统代理）
    └─ 判定 → 补救 → 记录（obs 环形 + JSONL）
        │
        ▼
  真实上游：官方 chatgpt 后端 / 用户中转站
```

包结构（新增以 `+` 标注，迁移以 `↔` 标注）：

```
internal/
├── proxy/         现有引擎（handler/stream/remedy/response/upstream）
│   + route.go     Route 快照 + SetRoute（从 helm-a DESIGN-PROVIDER-HOTSWAP §3 落地）
│   ↔ inject.go    增加 codebook / encode / deploy 三档开关
│   + reqlog.go    请求记录 JSONL（从 helm-a proxy/reqlog.go 移植）
├── protocol/
│   ├── jsonwalk/  现有（字节级，保留）
│   ├── sse/       现有 + 私有事件解析（rate_limits / response.metadata）
│   ↔ responses/   RequestView 增强：instructions 读写、LastUserTextParts、SpliceTextPart
├── inject/        ↔ 从 helm-a 平移（codebook.go / encode / sampler / store）
├── route/         + 供应商模型 + 迁移（旧单上游 → 一条 provider）+ 快照
├── secure/        + DPAPI（Windows）/ 明文拒绝断言（其它平台报错不静默）
├── obs/           + Store（环形+JSONL）/ Capture（配额+脱敏+index.jsonl）
├── codexcfg/      现有（TOML 行级模型 + 备份还原）→ 增加 sessions 视图、providers 视图
├── config/        现有（helmx.config.json）→ 增加 providers/mode/deploy/codebook/capture 段
└── svc/           现有 8 服务 → +ProviderService +CaptureService +SessionService
```

依赖方向（沿用 helm-a ARCH §4 的约束）：`svc → route/proxy/codexcfg/config/obs`；`proxy → protocol/inject/route`；`protocol` 不 import 兄弟包；所有 internal 不 import wails。

---

## 4. 迁移详单

### F1 供应商热切换（核心）

**问题**：现在上游由 codex 的 `config.toml` 决定，换供应商要改文件 + 重启 codex；`config.toml` 里的 `[model_providers.*]` 还能绕过代理（流量直连上游 → 注入失效）。

**做法**（照 helm-a `DESIGN-PROVIDER-HOTSWAP.md`，去掉隔离前提）：

1. `config.toml` 只写一个专用 provider，指向本地代理：
   ```toml
   model_provider = "helmx-local"
   [model_providers.helmx-local]
   name = "helmx-local"
   base_url = "http://127.0.0.1:1800/v1"
   wire_api = "responses"
   requires_openai_auth = false
   ```
2. 真实上游进 `internal/route`：`Route{Mode, BaseURL, Key, Model, Headers}`，`atomic.Pointer` 快照，**每请求读一次**。
3. 校验断言 **N1**：`config.toml` 里任何 `base_url` 非 `127.0.0.1:<listen>` → 自检 FAIL 并给出清理入口。
4. 旧配置迁移：把当前激活 provider 的 `base_url` 作为第一条 provider（`name: "当前上游"`），幂等。
5. 请求头策略红线（照抄 helm-a §6.1）：UA/`originator`/`session_id` 逐字透传；relay 模式**替换** `Authorization` 为供应商 key、**剥离** `chatgpt-account-id`/`cookie`。

**验收**
```
go test ./internal/route/...            # 快照语义：切换后新请求用新 base；进行中请求不受影响
go test ./internal/proxy/ -run TestProviderHotSwap   # 双假上游：手推 A→B，请求分别落到两个端口
grep -c "https\?://" ~/.codex/config.toml # 期望 0（本地地址除外）
```

### F2 DPAPI 凭据

`internal/secure`：`Seal([]byte) (string, error)` / `Open(string) ([]byte, error)`；Windows 用 `CryptProtectData`/`CryptUnprotectData`（`helm-a/internal/secure/secure_windows.go` 已实现，平移）；非 Windows 直接返回错误，**绝不回落明文**。

**验收**：`go test ./internal/secure/` + `! grep -q "sk-" %APPDATA%\helmx.config.json`（断言配置里无明文 key）。

### F3/F4 代号表与全文编码

平移 `inject/codebook.go` 与 encode（Latin-1 mojibake）。三条硬约束：
- 只作用于**最后一条 user 消息的 text part**，手术走 `jsonwalk`（A1：不重解析重排）；
- 解析失败原样透传并记 note（A3）；
- 与提示词注入**分开评**：`RequestRecord` 增加 `transforms: ["codebook:3","encode"]`，UI 分列展示。

**验收**：`go test ./internal/inject/ -run "Codebook|Encode"`；假上游断言 wire 里 user 文本已被替换、instructions 里出现映射段。

### F5 instructions 位注入（insert / append / replace-base）✅ 已完成

现在只往 `input[]` 插 system 消息；helm-a 的竞赛臂要求**顶层 `instructions` 整段替换**。已落地为 `internal/proxy/deploy.go` 三档：

| 语义 | 行为 | 落点 |
|---|---|---|
| `insert`（默认） | input[] 插 system 消息 | `InjectSystemAt`（不变） |
| `append` | 追加到顶层 `instructions` 末尾，保留官方 base | `InjectInstructions` |
| `replace-base` | 整段替换 `instructions`（覆盖官方 base_instructions 21.3KB） | `InjectInstructions` |

实现要点：全部走 `jsonwalk` 字节级手术（`ReplaceString`/`InsertFirst`，INV-4）；顶层没有 `instructions` 时造一个成员而不是放弃；幂等（补救重发会二次经过注入路径）；结构不认识 → 原样透传 + warn（INV-9）；`dry-run` 用同一语义计算并报告 `deploy`。

**验收（已跑）**
```
go test ./internal/proxy/ -run "Deploy|Pipeline"     → 全过（含 3 个管线级 wire 断言）
# 真机：二进制 + 假上游 tools/fakeupstream，配置 mode=ctf-ac / deploy=replace-base
REQ #1 path=/v1/responses body=28826 instructions=28451 hasProbe=false systemMsgs=0
#        ↑ 官方 base 被整段替换（探针串消失），input 无 system 消息（走 instructions 位）
```

**副作用修复（本次实测踩到）**：`helmx proxy` 默认改 codex 的 base_url，强杀会跳过收尾还原，把 codex 停在死端口。已加 `--no-apply`（只起代理、不改配置）并用它做上面的验证 —— 前后 `~/.codex/config.toml` SHA-256 一致。

**自检第 9 项「部署语义与模式匹配」**：`mode=ctf-ac` 但 `deploy=insert` 直接 FAIL。

### F6 调试报文捕获

照搬 helm-a `obs.Capture` 的形态（`Put(reqID, "13.upstream.request.body", data)`）：

```
%APPDATA%\helmx\capture\index.jsonl
%APPDATA%\helmx\capture\<日期>\<seq>-<reqID>\{request,upstream,response}.{meta.json,body}
                                            └ response.sse + frames.txt（流式）
                                            └ verdict.json
```

规则：默认关；开启后含凭据的头文件自动 `0600` + 目录 ACL 收紧；配额 512MB / 每日条数上限，LRU 删最旧；关闭开关**不清**已捕获文件；UI 常驻提示"报文含 API 凭据，勿外传"。

**验收**：`go test ./internal/obs/`（配额、脱敏、索引追加）；真机跑一条请求后目录树齐全。

### F7 请求记录持久化

`obs.Store` 环形 500（现状）+ 追加 `requests.jsonl`（按日轮转，最多 7 天）。`RequestService.Recent` 读内存，`RequestService.History(day)` 读盘。

### F13 提示词资产（✅ 已完成，只迁主臂）

helm-a 的提示词分三层；按用户 2026-11-22 的指示**只迁竞赛主臂**，A1/A2 两个生产短线档不入库：

| 层 | 资产 | 形态 | 现状 |
|---|---|---|---|
| ~~生产档 A1~~ | `settings.json` 的 `prompt`（299B） | `insert` | ❌ **不迁**（用户 2026-11-22 指示：只要主臂） |
| ~~生产档 A2~~ | `main.go` `defaultPrompt`（234B） | `insert` | ❌ **不迁**（同上） |
| 竞赛主臂 | `prompts/arms/g-composed-arm.md`（28.4KB，`deploy: replace-base`，`landed=true`） | 整段替换官方 `base_instructions`（内嵌官方 21.3KB） | ✅ **已注册为 `ctf-ac`「主臂」**：资产加密入 `internal/assets/data/prompt-a-composed.md`（28451B），装载依赖 F5（已完成） |
| 规则集 | `prompts/ROUTING-RULES-v3.md` v4（21 域 Intent-mapping，只有 `## RULES` 一节进臂） | 主臂组成部分 | ⏳ 已入库 `docs/prompts/ROUTING-RULES-v4.md` |
| 题库 | `prompts/questions/questions.txt`（62 行 / C01–C21 四档） | 跑批输入 | ⏳ 已入库 `tests/fixtures/questions-c01-c21.txt` |
| 资产台账 | `prompts/INVENTORY.md`（A–H 层盘点）、`MECHANISM-CATALOG` v4.6（§0 分类学 + §D 17 条禁入） | 决策依据 | ⏳ INVENTORY 已入库 `docs/prompts/INVENTORY-helm-a.md`；CATALOG 留在 helm-a |

**只迁主臂的理由**：主臂是 helm-a 唯一 `landed=true` 的现役臂，且它的实测前提（整段替换顶层 `instructions`）已由 F5 落地 —— 选它 + `deploy=replace-base` 才是被验证过的配置，svc 层对此有硬守卫（不匹配直接拒绝保存）。

**资产侧改动（已做）**
- `internal/assets/data/prompt-a-composed.md`：主臂正文 28451B（原文件 29065B 去掉 YAML frontmatter），逐字节等于 `docs/prompts/arms/g-composed-arm.md` 的正文段；
- `internal/assets/assets.go`：新增 `ctf-ac` 模式（`Deploy: "replace-base"`）+ 路径常量；模式总数 6→7；
- `tools/genassets/main.go`：`targetFiles` 增一项（否则 `go generate` 不加密它；当前共 10 个资产）；
- `internal/assets/assets_test.go`：模式数 6→7；`TestMigratedPromptAssets` 锁死 28451B 与开头段，并断言 `ctf-a1`/`ctf-a2` 已不可用。

**验收**
```
go generate ./internal/assets && go test ./internal/assets/ -run "PromptModes|MigratedPromptAssets" -v
# 真机端到端（二进制 + 假上游 + --no-apply）：
# REQ #1 path=/v1/responses body=28826 instructions=28451 hasProbe=false systemMsgs=0

### F9 会话浏览/删除

只读 `~/.codex/sessions/**/rollout-*.jsonl` + `state_5.sqlite` 的 threads 表（照 `CODEX-DB.md`）。删除 = 先复制到 `%APPDATA%\helmx\backup\sessions\<ts>\` 再删，SQLite 走维护锁；子代理会话归入主会话显示。

### F10 协议增强

- 平移 `responses.RequestView` 的能力：`LastUserTextParts` / `SpliceTextPart` / `AppendInstructionsTail`（现有 `request.go` 缺前三者）。
- SSE 增加私有事件识别：`rate_limits`（限流遥测）、`response.metadata`（会话归因）——只记录不改变转发。

### F11 netenv（出站代理）

平移 `internal/netenv`：env → Windows 注册表系统代理 → 直连。**这是真机踩坑**：系统代理只写注册表时，Go 默认 transport 直连公网，表现为 dial timeout。接进 proxy 的 `http.Transport.Proxy` 与 rewriter/riskgate 客户端。

### F12 矩阵 harness（HTTP 模式）

保留 `tests/helm-x-test-suite.py` 的题库口径，新增 `internal/proxy/prompt_matrix_test.go` 的 **HTTP 模式**（假上游 + arms × questions × runs），`matrix.jsonl` 落 `tests/jb/runs/<ts>/`。codex 模式（依赖隔离 home）不迁移。

---

## 5. UI 重设计

### 5.1 诊断（实测，不是感觉）

`python tools/contrast.py` 对现行深色主题的结论：

| 用例 | 比值 | AA 门槛 | 判定 |
|---|---|---|---|
| 正文 93% on 画布 12% | 16.51 | 4.5 | PASS |
| 元信息 `/60` | 6.14 | 4.5 | PASS |
| 元信息 `/45` | 3.83 | 4.5 | **FAIL** |
| 元信息 `/30`（App.vue / SidebarNav / Overview 各 1 处） | 2.27 | 4.5 | **FAIL** |
| 控件边框 hairline 7% white | **1.13** | 3.0 | **FAIL** |
| 卡片 22% vs 画布 12%（分离度） | 1.17 | — | 偏弱 |
| 分隔线 28% vs 12% | 1.39 | — | 偏弱 |

**结论**：文字本身够亮，但**输入框/选择器在深色里没有边界**（1.13:1 等于看不见），弱化文字有两档不合规，面板靠 1.17 的分离度"糊"在一起。用户说"看不清"是这三条叠加的体感。

### 5.2 方向（据两份调研）

- **浅色为默认**（JetBrains 风：中性灰阶 + 单一强调色），深色作为可选主题，两套都过 AA；
- **组件语言不变**（daisyUI 原语 + 现有 8 页 + 22 个前端测试），只换 token、布局与规则 —— 不引入 reka-ui/shadcn-vue（那会带来 100+ 组件与两套设计语言并存，见 §7 取舍）；
- 调研落地的硬规则（引 JetBrains 指南）：UI 基准字号 **13px**、只用内置字阶；**4px 基准网格**；图标 **16px（14×14 可视 + 1px 透明边）**；状态栏 12px / 工具窗 13px / 对话框 32px；**表格不画内边框**、斑马纹仅在区分数据集时用、悬停高亮整行、列可调宽；**tabs ≤8、Title Case ≤3 词**，内容不可用不删 tab、在正文解释；**进度 ≥1s 必须有可见进度**（<1s 不显示）；空状态 = 原因 + 动作（≤2 个）+ 帮助链接，容器工具栏在空状态有动作时隐藏；**不写 press/click 这类动词**，"add" 不写 "add new"；校验措辞替换表 Error→Problem / Failed to→Unable / Invalid→Incorrect / Must→Should be。

### 5.3 Token 表（全部实测达标）

**浅色（默认）**

| token | 值 | 用途 | 实测 |
|---|---|---|---|
| `--bg-canvas` | `#f6f7f9` | 窗口底 | — |
| `--bg-surface` | `#ffffff` | 面板/卡片 | 与画布 1.07（靠边框定形） |
| `--bg-subtle` | `#f0f2f5` | 表头/工具条 | — |
| `--fg` | `#1f2328` | 正文 | 14.74 / 15.80 |
| `--fg-muted` | `#57606a` | 次要文字 | 5.96 / 6.39 |
| `--fg-subtle` | `#6e7781` | 弱化（**下限**，不得更浅） | 4.55 |
| `--border` | `#e3e7ec` | 分隔线（装饰） | 1.24（信息项） |
| `--border-control` | `#7d8590` | 输入/选择器边框（**3:1 硬线**） | 3.73 |
| `--accent` | `#1a5fb4` | 主色/链接 | 6.29 |
| `--success` | `#1a7f37` | Healthy | 5.08 |
| `--warning` | `#8a5a00` | Refused | 5.93 |
| `--danger` | `#b42318` | Flagged/Cyber | 6.57 |
| `--selection` | `#eaf1fb` | 选中行 | 13.90 |

**深色（可选）**

| token | 值 | 实测 |
|---|---|---|
| `--bg-canvas` | `#16181d` | — |
| `--bg-surface` | `#1f232b` | 与画布 1.13 |
| `--fg` | `#e6e9ee` | 14.59 / 12.94 |
| `--fg-muted` | `#a7b0bd` | 7.19 |
| `--fg-subtle` | `#8b95a3` | 5.19 |
| `--border-control` | `#6b7482` | 3.33 |
| `--accent` | `#79b8ff` | 7.58 |
| `--success` | `#57d38c` | 8.32 |
| `--warning` | `#e5b567` | 8.35 |
| `--danger` | `#ff8a80` | 6.90 |

**字阶**：11（标签）/13（正文，默认）/15（面板标题）/20（页标题）/26（仅总览首屏数字）。行高 1.5；数字列 `font-variant-numeric: tabular-nums`。等宽：`Cascadia Code → Consolas → ui-monospace`（不引字体文件）。

### 5.4 布局与页面骨架

```
┌──────────┬───────────────────────────────────────────────┐
│ 240px    │ 页头 32px：标题 15px + 右侧动作（≤2 个）        │
│ 侧栏     ├───────────────────────────────────────────────┤
│ 分组：   │ 工具条 32px（搜索/过滤/视图切换）               │
│ 运行     ├───────────────────────────────────────────────┤
│ 调参     │ 主面板（表格/表单/报文树）                      │
│ 诊断     │                                               │
│          ├───────────────────────────────────────────────┤
│ 底部     │ 状态栏 24px：代理态 · 上游 · 请求数 · 版本       │
└──────────┴───────────────────────────────────────────────┘
```

页面（9 项；数字键 1–9，0 固定日志）：

| 键 | 页 | 内容 | 迁移来源 |
|---|---|---|---|
| 1 | 总览 | 状态条 + verdict 计数（同色小方点+数字，不用彩底大徽章）+ 最近 10 条 | 现有 Overview |
| 2 | 请求 | 全量历史（时间/路径/注入/判定/变换/补救/耗时）+ 展开 note + 报文入口 | 现有 + F7 |
| 3 | 报文 | capture 目录树：index → 时间线 → 四段报文 + SSE 帧表 | **F6（新）** |
| 4 | 提示词 | 7 档模式列表（含 `ctf-ac` 主臂）+ 正文预览 + 注入频率 + 部署语义（insert/append/replace-base） | 现有 Prompts.vue + F13 |
| 5 | 破甲 | 代号表（开关+槽位）/ 全文编码（互斥提示）+ 当前生效行 | **F3/F4（新）** |
| 6 | 供应商 | 卡片列表（官方/中转）+ 设为当前 + 连接检测 + 拉模型 + key 脱敏 | **F1/F2（新）** |
| 7 | 服务 | 代理启停/重试/UA 兜底/上游模式 | 现有 Services |
| 8 | 自检 | 8 项检查 + 一键复制报告 | 现有 Verify |
| 9 | 设置 | 主题（浅/深/跟随系统）、捕获配额与目录、日志位置、危险操作 | 新增 |
| 0 | 日志 | 运行日志（等宽 12px、自动滚底、上滚即停） | 现有 Logs |

### 5.5 反馈与文案

- **三条反馈线**（照 JetBrains）：banner = 页内常驻问题（如"配置里有 3 个定义不会生效"）；行内提示 = 字段级校验；dialog = 需要决策（写入/还原/删除）。短消息不做 toast，状态以面板数值更新为唯一完成确认。
- **危险操作**：按钮写动作（「写入 config.toml」「还原备份」「删除 3 个会话」），不出现"确定/取消"；写前弹 diff 预览。
- **空状态**：`还没有请求经过代理。确认 codex 的 base_url 指向 http://127.0.0.1:1800/v1 后，在 codex 里发一条消息。[复制命令] [打开配置]`（原因 + ≤2 动作 + 帮助）。
- **校验**：字段错误放上方、右移 40px；有错时提交按钮不亮；打开表单不报错。

### 5.6 可访问性与验收

- 对比度：`python tools/contrast.py` 退出码 0（纳入 CI 与 `Taskfile.yml`）；
- 键盘：全部可 Tab 到达、焦点环 2px 可见、数字键切页、Esc 关弹窗；
- 200% 缩放走查；`prefers-reduced-motion` 降级保留（现有已实现）；
- 三类证据分开报告：截图（布局）、键盘走查记录、对比度数值 —— 截图不能证明后两者。

---


**已落地（2026-11-22）**：

| 项 | 结果 |
|---|---|
| 对比度 | `tools/contrast.py --theme light` 0 项未达标；`--theme dark` 0 项（数值见 §5.3） |
| 截图 | `docs/preview-v5-overview.png` / `preview-v5-prompts.png` / `preview-v5-services.png`（真机 1568×1006） |
| 键盘 | 数字键 1–9 切页、0 回日志；焦点环 2px；Esc 关抽屉（沿用） |
| 动效降级 | `prefers-reduced-motion` 全关（v4 已有，v5 保留） |
| 未做 | 200% 缩放走查、读屏实测清单（需要人工，不在本轮） |

## 6. 阶段与提交切分

| 阶段 | 交付 | DoD（可执行） |
|---|---|---|
| **P0 基座**（1 周） | `internal/netenv`、`internal/secure`、`internal/route` + 设置迁移 + 自检新增 N1 断言 | `go test ./internal/{netenv,secure,route}/...`；`helmx verify` 9/9 |
| **P1 供应商热切换**（1 周） | config.toml 固化 `helmx-local`、proxy 用快照转发、ProviderService + 供应商页 | 双假上游手推 A→B；`config.toml` 无非本地 base_url；key 无明文 |
| **P2 注入面**（1.5 周） | codebook / encode / deploy 三档 + responses 视图增强 + 破甲页 | 假上游断言 wire；三种 deploy 各自单测；改造后 `go test ./internal/...` 全绿 |
| **P3 观测面**（1 周） | capture 树 + 配额/脱敏 + reqlog JSONL + 报文页 | 真机一条请求目录树齐全；`go test ./internal/obs/` |
| **P4 界面换血**（✅ done） | 设计系统 v5：浅色默认 + 深色可选（两套 token 实测达标）、13px 基准、状态栏、表格去内边框、主题切换；报文/破甲页已接线 | `python tools/contrast.py --theme light/dark` 各 0 项；`vitest` 26 项全绿（含 useTheme 4 项）；`npm run build` 通过；真机截图见 `docs/preview-v5-*.png` |
| **P5 会话与矩阵**（1 周） | F9 会话只读、F12 矩阵 HTTP 模式、F13 主臂装载（依赖 F5） | 会话列表可读；矩阵产物齐全；主臂在 replace-base 下 `instructionBytes≈28.4KB` |

合计约 **7 周**。每阶段独立可发布，P1 之后即产生用户价值（不用重启 codex 换供应商）。

---

## 7. 取舍与风险

| # | 取舍/风险 | 判断 |
|---|---|---|
| R1 | **daisyUI vs shadcn-vue** | 不引入 shadcn/reka-ui。理由：现有 8 页 + 22 测试已稳定，shadcn 带来 100+ 组件与两套设计语言；只移植**组件模式**（CodeBlock/Trace/ConfirmDialog/Hint），用 daisyUI 原语重写。唯一可选新依赖：`shiki`（代码高亮，按需） |
| R2 | 供应商模型里 key 与 codex 自带凭据混用 | 严格分模式：`official` 透传 codex OAuth 并剥离到第三方；`relay` 替换 Authorization、剥离 account-id/cookie（helm-a §6.1 已实测，作回归红线） |
| R3 | `replace-base` 覆盖官方 21k base_instructions | 仅竞赛/实验开关，默认关；UI 二次确认 + 显示字节数 + 一键回滚 |
| R4 | 捕获含凭据落盘 | 默认关；密钥脱敏默认开；目录 ACL 收紧；配额上限；开关关闭不清盘（避免"以为删了"） |
| R5 | 迁移旧配置（单上游 → providers） | 幂等迁移 + 失败回落旧字段；`config.json` 版本号自增 |
| R6 | 会话删除不可逆 | 先备份再删 + 维护锁 + 数量写进确认弹窗文案 |
| R7 | 界面重做打断现有测试 | 先做 token/布局（纯 CSS + 类名），再逐页接线；每页改完跑 `vue-tsc` + 该页相关 vitest |
| R8 | `wails3 task build` 当前坏（`build/darwin` 被删，icon 生成报 `open darwin/icons.icns`） | 先用 `go build -o bin\helmx.exe .`；本次迁移顺手恢复 `build/darwin/icons.icns` 或从 Taskfile 去掉 `-macfilename` |

---

## 8. 明确不做（迁移清单外）

- `internal/isocodex` 全部（下载器 / pinned 版本 / 登录 / 终端拉起 / 阶段状态机）；
- `RuntimeService`（Stage/EnsureExe/Launch/Login/Logout/Exec/CheckUpdate）；
- `settings.WorkDir / LaunchSandbox / LaunchApproval` 与 `--cd`/`--dangerously-bypass-*` 参数组装；
- `Codex.vue` 的「运行时」tab、概览的「一键启动」大按钮与 `isocodex:stage` 事件；
- 隔离 home 下的会话/技能/插件管理视图（改为读用户自己的 `~/.codex`）。

---

## 9. 三个默认值（已按最优解定，可否决）

| # | 决策 | 取定值 | 否决后的替代 |
|---|---|---|---|
| 1 | 主题默认 | **浅色为默认**，深色在设置里可选，跟随系统作为第三项 | 若坚持"跟随系统"，则 Windows 深色用户首屏仍是深色 —— token 已按 §5.3 备好，但"看不清"的体感只能靠深色 token 修，不能靠跟随系统修 |
| 2 | `replace-base` 是否进 UI | **不进 UI**，只走环境变量 `HELMX_DEPLOY=replace-base`（+ `verify` 显示当前部署语义） | 进 UI 需二次确认弹窗 + 字节数提示 + 一键回滚，多 0.5 天 |
| 3 | 会话删除（F9） | **本期做只读浏览，删除延后**（读 `state_5.sqlite` 要引 sqlite 依赖，成本高于收益） | 本期就做删除 → 需备份 + 维护锁 + 确认弹窗，多 1 天 |

---

## 10. 参考

- `helm-a/docs/ARCH.md`（功能全集/依赖方向）、`DESIGN-PROVIDER-HOTSWAP.md`（F1 原稿）、`CODEBOOK-DESIGN §11.5`（F4 现行规格）、`MASTER-PLAN.md`（机制分类学与禁入清单）；
- `E:\Downloads\大项目 UI_UX 设计文档调研.html`（WCAG 4.5/3.0 底线、token 流水线、Do/Don't 成对配图、术语立法）；
- `E:\Downloads\JetBrains UX 指南深挖.html`（13px 基准/4px 网格/16px 图标、tabs ≤8、表格无内边框、空状态公式、校验措辞替换表、1 秒进度门槛）；
- 本仓库：`tools/contrast.py`（对比度审计，可作 CI 断言）。