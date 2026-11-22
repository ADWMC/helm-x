# 变更记录

## v0.3.0 — 2026-11-22

### 修复：代号表会把技术标识符切断 + 「槽位」看不懂
- 切片原来是纯随机 2–4 字符，实测会把 `IOCTL` 切成 `OCTL`、`base_url` 切成 `ase_url` —— 模型拿到不存在的名字，答案直接跑偏。
- `internal/inject`：新增 `asciiRuns()`，把 ASCII 标识符/路径/命令（长度 ≥2 的 `[A-Za-z0-9_\-.]` 连续段）标为受保护位置，切片起点或终点落在其中就重抽。回归测试 `TestCodebookKeepsIdentifiers`（slots 1–4 全档断言 `IOCTL` / `base_url` / `/v1/responses` 原样保留）。
- 界面：「槽位」→「每条请求替换 [N] 个片段」，并加 tooltip「每条命中的请求抽几个片段换成伪词（1–8）」。
### 文档：把「特此说明」提到 README 首屏
- 原实现把"本项目由 AI 生成"藏在文末 License 段。现在提到首屏（Star/群链接之后、截图之前），只保留一句：> **特此说明**：本项目由 AI 生成（DeepSeek Harness + Mimo V2.6 Pro），代码质量仅供参考。
- 许可与免责仍留给文末 License 段，不塞进声明里。
- v0.3.0 Release 正文同步追加同一句。
### 文档：README 删掉「测试结果」
- 移除 50 题基准表（通过 86% / 网关拦截 14% / 模型拒绝 0% / 有效应答 100%）与口径说明：逐题报告含模型原文不入库，留着无法自证，反而像营销话术。
- 目录同步删条目；章节 14 个、目录 13 条，逐条对齐。
- 基准与复现方式仍在 `tests/`（跑批脚本 + 基线 JSON），需要时自己跑。
### 文档：README 第二轮（去 emoji、图减到 2 张、营销区上移）
- 全篇去掉图标 emoji：能力表从"图标 + 名称 + 说明"三列改为两列，数据流图改用纯 ASCII（`--->` / `|` / `+--`），不再依赖字形宽度。
- 截图从 7 张减到 2 张（总览做 hero、请求页做正文配图），其余留在 `docs/screens/` 供需要时点开。
- 新增首屏营销区：一句话定位 -> Star 号召 + Telegram / QQ / Issues 三个直达链接 -> 一张总览图。原来放在文末的「社区」章节合并到这里，文末不再重复。
- 删掉只在会话里讲的解释性内容：不变量表（INV-2/4/5/6/8/9）不再进 README，贡献段改成一句链接指向 `docs/DESIGN.md`；「三件事和代理本身同样重要…每个开关都能解释它为什么存在」这类写作说明整段删除。
- 目录与章节逐条核对：14 条全部命中，无多余、无缺失。
### 文档：README 徽章换成 Shields.io（静态 + 动态）+ 仓库元数据归位
- 徽章由 5 个手写静态牌升级为 11 个：license / **动态 repo version** / release tag / stars / forks / issues / last-commit / go-mod 版本 / Wails v3 / platform / single-exe，统一 `flat-square` 与主色 `2ea44f`。
- 动态牌用的是 Shields.io 的 `badge/dynamic/json`：数据源指向 `cdn.jsdelivr.net/gh/ADWMC/helm-x@master/wails.json`，`query=$.productVersion` —— 推一次 `wails.json` 就自动变，不用手改 SVG。
- 逐条实测 11 个端点全部 HTTP 200，动态牌当前渲染 `0.2.1`（远端 master 的真实版本；本地 0.3.0 未推）。
- 仓库描述原本停留在"C++17 零依赖"（早就不成立），已改为"本地映射代理 + 桌面控制台 … Go + Wails v3"；补 6 个 topics（codex / local-proxy / golang / wails / desktop-app / observability）。
- 新增「贡献」段：三条可执行命令（go test / vitest / verify）+ 四条 PR 前置条件（不变量、不要第二个 .syso、截图+对比度、新开关要答得出"解决什么/代价是什么"）。
### 文档：README 重写（参照 HMCL / GitHub README 规范）+ 真实界面截图
- 结构改成"一屏能看懂 + 逐层下钻"：徽章 → 一句话 → 数据流 → 能力表 → 测试结果 → 快速开始 → 9 页界面（截图网格）→ 工作原理与不变量 → 改了哪些文件 → 提示词表 → 破甲两开关 → 命令行 → 构建 → 目录 → 已知限制。
- 删掉原先铺满半屏的多张基准明细表，收敛为一张 headline（86% 通过 / 0% 模型拒绝 / 排除网关拦截后 100%），明细指向 `tests/` 复现。
- 新增 `docs/screens/` 七张真机截图（总览 / 请求 / 报文 / 自检 / 服务 / 提示词 / 破甲），统一 1200px、合计 1.0 MB；README 用 HTML 表格排成 2×3 网格。
- 修正过时表述：删「10 页」「改写器」「托盘」，补「关窗即退出并还原」「Wails 事件不通 → SSE + 轮询」「exe 字符串版本信息仍空」等实际状态。
### 删除：改写器（LLM 语义改写，整条链）
- 删 `internal/rewriter/`（`rewriter.go` / `local.go`）、`internal/svc/rewriter.go`（Get/Set/Test）、`internal/assets/data/rewrite_prompt.txt` 与 `assets.RewritePrompt()`、`tests/rewrite_fidelity.py`。
- 补救阶梯第 1 级回到"只重建会话、不换措辞"：Flagged/Refused 重发一律用原文，`remedyDeps.rewriter` 与 `proxy.Rewriter` 接口一并删除（`remedy.go` 里那段"先语义改写再重发"的注释保留为历史说明）。
- 配置：`config.RewriterSettings`、默认值、normalize 段、`SettingsView.RewriterAPIKeySet` 全删；`/api` 侧的 `rewriterGet/Set/Test` 与 `RewriterView/RewriterInput` 类型删除；Wails 绑定重新生成（7 个服务）。
- 界面：删「改写器」页、导航项、路由、页面标题、`rewriter` 图标；页面数 10 → **9**；README 的功能列表、目录说明、参考项目同步。
- 理由：它只在 Flagged 分支生效、需要外部 LLM key 才能验证，实测从未被端到端确认过；职责又和提示词注入重叠（都在改"请求里说什么"）。留着只是维护成本与一处未验证的失败面。

### 变更：主题默认改为跟随系统
- 原来默认硬编码浅色（`useTheme` 的 `read()` 兜底就是 'light'），并且把窗口标题栏强制成 `application.Light` —— Windows 深色模式下首屏仍是白底，用户问"为什么不按系统切"。
- 现在：默认偏好 = `system`（首次启动跟随 Windows），标题栏 `Windows.Theme = application.SystemDefault`，两边一致；显式选过浅色/深色才覆盖，选择存 localStorage。
- 「服务」页「外观」卡：三档顺序改为 **跟随系统 / 浅色 / 深色**，页头显示 `跟随系统 · 当前 dark`。
- 实测：本机 Windows 为深色 → 启动即深色渲染并通过截图核对；两套 token 仍各 0 项未达标（`python tools/contrast.py --theme dark`）。
- 框架限制（无法消除）：Wails v3 无运行期改标题栏主题的公开 API —— 系统深色而用户把内容显式切成浅色时，标题栏仍是深色。
### 修复：四个遗留缺陷（wails3 构建 / 版本资源 / 强杀不还原 / 图标与子系统）
- **`wails3 task build` 恢复可用**：根因是 `build/darwin/` 被清理后，图标任务仍无条件写 `darwin/icons.icns`。已重建 `build/darwin/icons.icns`（`wails3 generate icons` 生成，718KB），现在 `wails3 task build` 能一路走到链接。注意：不要同时在根目录放第二个 `.syso`，否则 `too many .rsrc sections`。
- **exe 版本资源可读**：wails 的 syso 把 RT_VERSION 写成语言 0x0000（中性）。Win32 探针定位到三处：`\` 根块 OK、`\StringFileInfo\000004B0\ProductVersion` 能取到、`040904B0` 取不到 —— 目录项语言、块内文本键、VarFileInfo Translation 全是中性。新增 `tools/verlang` 做三处等长改写（目录 0x0000→0x0409、键 000004B0→040904B0、Translation DWORD），并把 VS_FIXEDFILEINFO 的产品版本对齐到文件版本。实测 `FileVersionRaw=0.3.0.0`、`ProductVersionRaw=0.3.0.0`、Language=英语(美国)。
  - 仍缺：StringFileInfo 的字符串字段（产品名称/说明/公司）在 .NET 的 `FileVersionInfo` 里仍为空 —— 直接 `VerQueryValue` 能取到值，说明块内结构被框架的枚举路径跳过。待后续换生成器或修 syso 写出逻辑。
- **强杀后不再把 codex 留在死端口**：新增启动自愈 `internal/runtime/heal.go` —— 若激活 provider 的 `base_url` 指向回环地址、该端口 800ms 内连不上、且存在还原点，则自动还原并记日志；没有还原点时只告警。实测：把 base_url 改成 `127.0.0.1:1899`（无人监听）后启动 helmx，配置被还原成 `https://huablog.org/v1`，日志「检测到上次异常退出…已还原 codex 配置」。
- **图标 + 子系统**：`scripts/build.ps1` 现在四步（前端 → syso 含图标/清单/版本 → `-H windowsgui` 链接 → verlang 修正版本资源语言）。`ExtractAssociatedIcon` 取到绿色 H，PE Subsystem=2，双击不弹控制台。
### 新增：请求历史实时化（loopback SSE）
- 之前不是实时：`Requests` / `Overview` 都是 5 秒轮询 —— 因为 Wails 的 `proxy:request` 事件在本机 WebView2 下收不到（见下一条修复）。
- 代理新增只读实时流 `GET /__helmx/events`（`internal/proxy/live.go`）：每完成一个请求就把 `RequestRecord` 以 SSE 推给订阅者；连上时先回放最近 200 条，前端按 `id` 去重。
- 三条约束：只在回环地址（代理本来就只监听 127.0.0.1）、只读、慢客户端丢帧不阻塞转发主链路（缓冲 64，满即弃）。
- 前端 `composables/useRequestStream.ts`：`EventSource` 连不上就关掉自己，页面继续用 5 秒轮询兜底 —— 两页现在都是"SSE 主通道 + 轮询兜底"。
- 验收：`curl -N http://127.0.0.1:1820/__helmx/events` 订阅后发一条请求，SSE 立刻收到 `{"id":"req_1",...,"class":"Healthy"}`；Go 单测 5 项（派发/丢帧/回放上限/本地处理/不误伤普通路径）、前端 3 项（URL 拼接、坏帧忽略、断线自停）。
### 变更：关窗口 = 退出并还原（托盘移除）
- 原行为：点关闭 → `e.Cancel()` + `window.Hide()`（托栏继续跑），托盘菜单才能退出。实测三层都不成立：
  1. 本 Wails beta + WebView2 下取消关闭**保不住窗口**：原 HWND 被销毁，只剩 202×56 残窗（rect 实测 `114,114,316,170` / `342,342,544,398`），`Show()`/`UnMinimise()`/原生 `ShowWindow(SW_RESTORE)` 全恢复不回来；
  2. 再叠 `DisableQuitOnLastWindowClosed: true`，应用就变成"窗口没了、进程还在、界面再也打不开"；
  3. 托盘图标本身也不可靠（会被折叠进溢出区、explorer 重启后丢失）。
- 现在：点关闭 = 正常退出，`WindowClosing` 里同步 `shutdown()` 还原 `config.toml`（原来只在 `OnShutdown` 还原，实测这条路径没被调到 —— 关掉后 `base_url` 停在死端口）。
- 实测：关闭前 `base_url=http://127.0.0.1:1800/v1` → 关闭后 `https://huablog.org/v1`，进程退出，1800 释放，日志「退出时已还原 codex 配置」。
- 托盘的「显示控制台/启停代理/还原并退出」菜单一并移除；需要无窗口常驻时用 `helmx proxy`（Ctrl+C 自动还原）。
- 第二实例：窗口开着时再双击 exe，把已有窗口提到前台。
### 修复：exe 没有图标、双击弹控制台
- 图标：原来 `go build` 出包不带 Windows 资源 —— 资源管理器里是默认空白窗口图标。`wails3 generate syso -icon build/windows/icon.ico -manifest ...` 生成 `helmx.syso` 后再链接，exe 现在带绿色「H」图标（`ExtractAssociatedIcon` 实测 48×48）。运行期窗口/托盘图标一直是对的（`build/appicon.png` 经 Wails 注入）。
- 子系统：之前是控制台（PE Subsystem=3），双击先弹黑窗。改为 `-H windowsgui`（Subsystem=2），并加 `console_windows.go`：命令行子命令启动时 `AttachConsole(ATTACH_PARENT_PROCESS)`，且**只在 std 句柄无效时**才重建 CONOUT$ —— 否则 PowerShell 的 `&` 管道重定向会被绕过，`helmx verify` 一个字都收不到（实测踩到）。
- 新增 `scripts/build.ps1`：前端构建 → syso（图标+清单）→ `go build -a -H windowsgui`。`-a` 是必需的：Go 构建缓存不跟踪 `.syso` 变化，不加会复用旧归档、图标进不去。
- README 构建段改为指向该脚本，并写明 `wails3 task build` 当前为何不可用。
- 已知缺口：exe 的**版本资源**仍为空（`wails3 generate syso -info` 产出的 `VS_VERSION_INFO` Windows 读不出，两条格式都试过）；版本号在界面状态栏与 `wails.json`。
### 版本号：0.2.2 → 0.3.0
- 四处来源同步：`frontend/package.json`、`frontend/package-lock.json`（根 + packages[""]）、`wails.json`（顶层 + `info.productVersion`）、`build/windows/info.json`（`file_version` + `ProductVersion`）。
- `CHANGELOG.md` 从「未发布」改为 `## v0.3.0 — 2026-11-22`。
- 已知缺口（两条，都不影响界面显示的版本）：exe 的 Windows 版本资源仍为空 —— `wails3 task build` 坏在图标步骤（`build/darwin` 缺失），手工 `wails3 generate syso` 生成的 `.rsrc` 段进得去但 Windows 读不出 `FileVersionInfo`；Go build info 仍是 `v0.2.2-0…+dirty`，因为仓库最新 tag 是 `v0.2.1` 且工作区未提交。要两者都对，需要先提交再打 `v0.3.0` tag、并修好 `wails3` 构建链。
### 修复：自检页永远停在「执行中」（事件通道不通）
- 现象：点「运行自检」后 9 项结果一行都不出现，按钮永久 disabled；日志里 Go 侧已经写完「自检完成：0 项失败」。
- 根因：结果只靠 `proxy:verify` 自定义事件从 Go 推到前端，**本机 WebView2 下一条都没到**（事件通道未定位，见已知问题）。
- 改法：把状态放到服务端 `VerifyService.Checks()`（`running/total/failed/items`），前端每 350ms 轮询增量；顺带修掉"切走再回来结果丢失"（原来 rows 是组件局部状态）。事件保留但不再是唯一通道。
- 同因修复：`Logs.vue` 也只用 `proxy:log` 事件且文案写"实时推送，无需刷新" → 改为每 2 秒拉尾部 500 行，文案如实写明；`Overview.vue` 的"最近请求"加 5 秒兜底轮询。
- 新增 `docs/screens/04-verify.png`（修复后 9/9 全 PASS 的真机截图）。

### 界面：图标去重 + 去掉 UI 里的解释文字
- 图标：`报文` 原来复用 `requests`、`破甲` 原来复用 `verify`（同一个图标出现在两个页面上）。新增 `capture`（三层叠片）与 `armor`（盾+勾），删除未使用的 `context`；现在 10 个页面 = 10 个图标，无复用、无闲置。
- 文案：页面标题下的"这页干什么"说明（`PAGE_META.desc`）整体删除，标题自带信息；破甲页的「改写管线」三行 bullet、提示词页的落点说明/注入频率长段/预览尾注、服务页的 UA 与守护解释、改写器的一行解释、报文页的落盘说明、自检页的检查项罗列 —— 全部删掉或压成一行事实。这些是讲给评审听的设计理由，只在会话里讲，不留在产品界面。
- 保留：会改变行为的提示（replace-base 覆盖官方 base、encode 与代号表互斥、凭据脱敏）、空状态的动作指引、错误信息。
### 界面：设计系统 v5（浅色默认，实测达标）
- 换掉 v4 暖黑主题：实测三项不合规 —— 控件边框 **1.13:1**（输入框在深色里等于没边界）、`/30` 文字 2.27:1、`/45` 文字 3.83:1。正文本身 16.5:1，所以问题不是"字太暗"，是"边界没了"。
- 新 token（浅色默认 / 深色可选，两套都实测）：正文 15.80/12.94、次要 6.39/7.19、弱化 4.55/5.19、**控件边框 3.73/3.33**、强调 6.29/7.58、危险 6.57/6.90。
- 排版与密度按 JetBrains 指南：UI 基准 **13px**、4px 网格、控件高 26/28px、**状态栏 24px**、顶部栏 40px、图标 16px。
- 表格：**不画内边框**（只留表头下一条线）、悬停整行、斑马纹关闭（同质行不需要条纹）。
- 圆角：首版用 rem 写在 13px 根字号上被一起缩小（卡 4.9px / 控件 3.3px），观感"太方"；改为 px 直给 —— **卡片 12px、控件与导航项 8px**，并给卡片补一层极轻投影（只负责"高出画布一层"）。
- 主题切换：`composables/useTheme.ts`（light / dark / system，localStorage 持久化），入口在「服务」页「外观」卡；窗口标题栏固定浅色（Wails v3 无运行期改标题栏主题的公开 API，已知不一致）。
- 新增底部状态栏：监听地址 / 上游 / 请求数 / 版本，只放事实。
- 低对比类清理：`text-base-content/30`、`/45`、`/55` 全部抬到 `/70`。
- 审计脚本 `tools/contrast.py` 扩 `--theme light|dark|current`，两套候选各 0 项未达标。

### 新增：请求层改写（代号表 / 全文编码）
- `internal/inject`：纯算法层（`Mojibake`/`Demojibake`、代号表 `Apply`+`MappingSegment`），不碰 HTTP/JSON。
- `internal/proxy/transform.go`：报文级手术 —— 逐个 text part 用 `jsonwalk` 定点替换（INV-4），映射段/还原说明 append 进 `instructions`。
- 两条通道互斥，**encode 优先**；都关时请求体逐字节不变。结构不认识 → 原样透传 + note（INV-9）。
- `RequestRecord.transforms` 记录 `codebook:N` / `encode:N`；配置三项 `codebook_enabled/codebook_slots/encode_enabled`（slots 1–8，默认 2）。
- 前端新增「破甲」页（开关 + 槽位 + 互斥说明）。

### 新增：调试报文捕获 + 请求记录落盘
- `internal/obs`：`Capture`（`%APPDATA%\helmx\capture\<日期>\<reqID>\{00..04}` + `index.jsonl`，512MB 配额按日 LRU，凭据默认脱敏）与 `Recorder`（`%APPDATA%\helmx\records\requests-<日期>.jsonl`，保留 7 天）。
- 接线四点：入站元信息+原文、发往上游的报文（注入与改写后，可与原文对照）、响应正文、判定；流式只落窗口内头部与累计正文（O(1) 转发不变）。
- 新增 `CaptureService`（State/List/Files/Read/OpenDir + 开关与脱敏热切换）与「报文」页。
- 捕获是旁路：失败只回调错误，绝不影响转发。

### 新增：上游私有 SSE 事件解析
- `internal/proxy/events.go`：识别 `rate_limits` / `response.metadata` / `response.completed`，摘录进 `RequestRecord.upstreamMeta`（单条 ≤512B；`completed` 只取 id/status/usage，不复制正文）。仅观测，不参与判定。

### 新增：出站代理探测
- `internal/netenv`：env → Windows 系统代理（HKCU 注册表）→ 直连；接入上游 `http.Transport.Proxy`。只配系统代理的机器不再直连公网（表现为 dial timeout）。

### 新增：提示词部署语义三档（整段替换模式）
- `internal/proxy/deploy.go`：`insert`（默认，input[] system 消息）/ `append`（追加到顶层 `instructions`）/ `replace-base`（**整段替换**官方 `base_instructions`）。
- 全走 `jsonwalk` 字节级手术：只替换 `instructions` 值区间，其余字段一字节不动（INV-4）；顶层缺该字段时造成员；幂等；结构不认识则原样透传并记 warn（INV-9）。
- `dry-run` 报告新增 `deploy` 字段，并按同一语义计算（不再一律按 insert 算）。
- 配置新增 `deploy`（白名单校验，未知值回落 insert）；`svc.PromptService` 新增 `GetDeploy`/`SetDeploy`；提示词页新增「部署语义」卡（三选一 + replace-base 警示 + 主臂徽章）。
- 守卫：选 `ctf-ac` 而部署语义不是 replace-base 时**拒绝保存**并给可执行提示；自检新增第 9 项「部署语义与模式匹配」（`helmx verify` 现为 9 项）。
- 主臂上线：`ctf-ac`「主臂」= helm-a `g-composed-arm`（28451B，`deploy: replace-base`，`landed=true`），加密入资产。
- 真机端到端（二进制 + 假上游）：`instructions=28451 hasProbe=false systemMsgs=0` —— 官方 base 被整段替换、input 无 system 消息。

### 修复：`helmx proxy` 强杀后 codex 停在死端口
- 新增 `--no-apply`：只起代理、不改写 `config.toml`（对着假上游验证时用）；默认行为不变。
- 端到端验证改用 `--no-apply`，前后 `~/.codex/config.toml` SHA-256 一致。（实测踩到：强杀跳过收尾还原，codex 的 base_url 停在 1806 死端口，用 `helmx proxy --restore` 修回。）

### 新增：假上游工具
- `tools/fakeupstream`：收一个请求，打印 `instructions` 长度与 system 消息数，返回最小合法响应；用于"发出去的到底是什么"这类断言（不联网、不烧额度）。

### 新增：提示词资产（从 helm-a 迁移，只保留主臂）
- `ctf-ac`「主臂」28451B —— helm-a `prompts/arms/g-composed-arm.md`（原文件 29065B，去掉 YAML frontmatter），`deploy: replace-base`、`landed=true`，加密入 `internal/assets/data/prompt-a-composed.md`。
- 只保留主臂：A1（299B 生产严格档）/ A2（234B 完成契约档）已按用户要求移除，不入库、不注册模式。
- 主臂原文与路由规则集 v4 另存 `docs/prompts/`（只读素材，不进二进制）；`replace-base` 部署语义已落地，主臂可直接装载。
- 题库 `C01–C21`（62 行）入 `tests/fixtures/questions-c01-c21.txt`；helm-a 资产台账入 `docs/prompts/INVENTORY-helm-a.md`。
- 测试：模式数 6→8；新增 `TestMigratedPromptAssets` 锁死 299/234 字节数（文本改一字，原实测结论作废）。
- 资产清单 `tools/genassets/main.go` 同步增项，`go generate ./internal/assets` 重新加密。

### 文档
- 新增 `docs/MIGRATION-HELM-A.md`：能力迁移清单（17 项：迁 13 / 不迁 4）、阶段与验收、UI 重设计的实测 token 表。
- 新增 `tools/contrast.py`：WCAG 对比度审计（现行深色 3 项不达标；候选浅/深两套 0 项）。

## v0.2.2 — 2026-11-22

### 删除：风险闸门（整个功能，前后端一起清）
- 删 `internal/riskgate/`（gate / client / l0 / cache / fidelity / prompt / types）、`internal/svc/riskgate.go`、
  `internal/proxy/gate_test.go`、`tools/riskgate-eval/`。
- 删管线挂点：`handler.go` 的 1.5 段（外科式改写）、`ConfigProvider.Gate()`、`RequestRecord.Gate`、
  `runtime.Gate()`、`config.RiskGateSettings` 与 `RiskGateDefaults()`、svc 服务注册与前端绑定。
- 删随闸门引入、无其他调用方的两个文件：`proxy/rewrite_user.go`（`ReplaceLastUserText`）、
  `proxy/auxclass.go`（`IsAuxiliaryRequest`）。
- 删前端：`pages/RiskGate.vue`、`components/GateBadge.vue`、`composables/useGateStyle.ts`(+spec)、
  导航项 / 路由 / 页面元信息 / 请求页闸门列 / 总览三块指标 / `api.riskGate*`。
- 删 `docs/RISKGATE-DESIGN.md` 与 `tests/build_replay.py`（后者的唯一消费者是 riskgate-eval）。
- 理由（实测）：`enforce` 要求 `recall ≥ 0.80`，实测 0.67；功能始终停留在 `shadow` 只记录不改写，
  等于给每条主请求加一次判定开销换一行日志。请求改写同理 —— 该走的路径提示词已经在做。

### 修复：数字键 0 一直无效
- `App.vue` 原写 `e.key === '0' ? 9 : ...`，`HOTKEY_ORDER` 只有 9 项（下标 0–8），0 恒为空操作。
  现在 0 固定跳「运行日志」，1–8 依次对应导航顺序。

### 版本号
- `frontend/package.json` / `wails.json` 同步到 0.2.2；顺带修掉 `wails.json` 里
  `info.productVersion` 仍是 `0.1.0` 的陈旧值（与顶层 `productVersion` 不一致）。

## v0.2.1 — 2026-10-28

### 新增：请求侧风险闸门（可选，默认关闭）
- `internal/riskgate`：本地快筛（L0）→ 缓存 → 模型判定（流式早停）→ 保真检查，任何失败一律直通，绝不阻断请求。
- 判定分两轴：`filter`（上游 cyber 过滤）与 `compliance`（模型是否肯交付）。实测 `filter=high` 覆盖实际拒答 7/8。
- 改写按类处理：规避语义 → 观测面对抗；真实产品名 → `SERVICE` slot。保真检查要求技术实体逐字保留，
  产物名词（注册机/驱动/生成器）不允许被 slot 化。
- 前端：新增「风险闸门」页（配置 / 判定预览 / 口径说明）、请求页闸门列、总览三块指标。
- 默认 `shadow`（只记录不改写）；`enforce` 需先满足 `recall ≥ 0.80`，当前实测 0.67，故仍关闭。

### 修复：工具"撒谎"的三处（来自无上下文用户审查）
- `--no-retry` 此前无效：横幅写"重试: 已禁用"，上游仍被打 11 次、客户端等 32 秒。
  根因是 `retryOpts()` 只在 `Config==nil` 时用启动参数。现在 CLI flag 优先于运行时配置，实测 1 次 / 3 秒。
- 上游 `200 + 空体` 此前返回「HTTP 200 + 错误 JSON」，状态码撒谎。合成错误体或 Malformed 时统一 502。
- `verify` 此前在 `base_url` 指向死端口时仍 6/6 全绿（`apply` 单独执行或进程被强杀都会造成）。
  新增「代理态一致」检查：指向本机端口时必须 800ms 内真连通，否则 FAIL 并给出下一步。

### 修复：还原会吃掉用户配置
- `remove` / 「移除注入并还原」此前用（可能很旧的）完整备份整文件覆盖 `config.toml`，
  实测会丢 `model`、`requires_openai_auth`、`notify`、`[projects.*].trust_level`、`[plugins.*]`。
- 现在默认**拒绝**并列出会丢的键；`--force` 才覆盖。

### 修复：静默降级
- 配置里残留已删除的提示词模式时，`assets.Prompt` 会静默回落默认模式，界面看不出差别。
- 新增 `verify` 第 8 项「提示词模式有效」，未知模式直接 FAIL。

### 改动：提示词
- 六个人名：铁面（经典计分）· 阿契（完成契约）· 短刃（轻量沙盒，默认）· 老炮（厚人设）· 账房（事务验收）· 总管（沙盒融合）。
- 删除全部外部对照提示词（MDX v6.1/v45、海鸥 3.0、xiaoliu、xw9114），只保留自有六个模式。
- 卡片不再写通过率，改为「哪些无影响 / 哪些无法绕过」：创作、暴力、逆向、横向无影响；
  内核过 ACE、域控提权、注册机/人机验证在三个模式上都无法绕过（OpenAI 加强了模型防御）。
- 改写器提示词按实测重写：删掉与实测反向的敏感词表，改为两类触发器分开治 + 输出前自检；保真率 33% → 100%。

### 删除
- 「上下文」页（前后端一起清；只展示 codex 自己的三个压缩键，零可设项）。
- 「Steam 注册」页（测试用例的历史产物：模型拒绝后给的替代品被做成了页面）。

### 可读性
- 主题：画布/卡片/边框亮度分离度 1.05:1 → 1.20:1；正文基准 16px → 17px。
- 低对比度文字（`/30`–`/50`，实测 2.3–4.5:1）抬到 `/55`–`/70`（6.1–8.0:1）；≤11px 字号整体 +1 档。

### 构建
- 版本号单一来源：`frontend/package.json` → `vite define` → 侧栏渲染；`wails.json productVersion` 同步。

## v0.2.0 — 更早
见 git 历史。
