# 上下文 / 记忆污染：入口清单

> 2026-11-22。范围修正：这份文档只回答一件事 —— **攻击者能把内容塞进模型的"上下文/记忆"的入口在哪**（落盘的、每轮重新注入的、以及从上游下发的），不讨论"用户文本 → 系统指令"这类提权分析（那部分见 §附）。
> 对象：本机 `~/.codex`（codex 0.167.0）+ `helm-x` + `helm-a`。

## 0. 结论（三句话）

1. **最直接的入口不在本机，在上游**：模型目录（`croli-model-catalog.json`，488 KB）里带 `base_instructions`(17,730 字) 与 `model_messages.instructions_template`(21,620 字)，实测这些文本就是 codex 发给上游的 `instructions`。**换一个中转站 = 换掉模型的系统提示词**，不需要碰用户机器。
2. **本机记忆层是全开的文件系统**：`~/.codex/memories/` 有 `MEMORY.md`(8.8 KB) / `raw_memories.md`(9.9 KB) / `memory_summary.md`(3.1 KB，"User Profile" 摘要)，另有 `memories_1.sqlite` 与 `memories_v2_1.sqlite` 的 `stage1_outputs(raw_memory, rollout_summary)`。任何能写这些文件/行的人，都在改写"模型记住的你"。
3. **helm-x 与 helm-a 都不写这些面**（0 处引用）；helm-x 唯一相关的动作是 `replace-base` 会把上游下发的 `base_instructions` 覆盖掉 —— 这是副作用式防御，不是设计出来的。

## 1. 每轮重新注入的输入（不落盘也算上下文）

| 编号 | 入口 | 实际路径 / 字段 | 谁控制 | 现状 |
|---|---|---|---|---|
| C1 | **上游模型目录** | `~/.codex/<provider>-model-catalog.json` → `models[].base_instructions` + `model_messages.instructions_template` | **中转站**（不可信） | **实测在 wire 上命中**：捕获的 `01.request.body`(92,535 B) 含 "You and the user share one workspace" |
| C2 | 客户端 `instructions` | 请求体顶层字段 | 客户端 / 插件 / MCP | `insert`/`append` 原样透传（实测 `ATTACKER-SYSTEM-TEXT` 到达上游） |
| C3 | `AGENTS.md` | `~/.codex/AGENTS.md`（0 字节）、工作区及各级父目录 | 任何能写文件的人 | 本机目前为空；`project_doc_max_bytes` 未在配置中设置 → 走默认 |
| C4 | `SKILL.md` 目录 | `~/.codex/skills/**`（49 文件，含 `.system/` 内置技能） | 安装/下载 | 技能目录以"## Skills"段落形式进上下文 |
| C5 | 插件包 | `~/.codex/plugins/**`（2,163 文件）+ `plugins."<name>@<source>"` 14 项 | 安装/下载 | 插件携带 skills + MCP，全部进上下文 |
| C6 | MCP server | `config.toml [mcp_servers.*]`：`antumbra-debug`、`iida`、`native_http_bridge`、`node_repl` | 本地配置 / 安装流程 | 工具名与描述进上下文；`node_repl.env` 里还嵌了本地脚本路径 |
| C7 | 通知/其他键 | `notify = [...]`、`show-context-window-usage` 等 | 本地配置 | 低风险 |

## 2. 落盘的记忆层（持久污染面）

| 编号 | 存储 | 内容 | 写入者 | 现状 |
|---|---|---|---|---|
| M1 | `~/.codex/memories/MEMORY.md` | 蒸馏后的"用户画像/长期记忆" | codex 记忆管线 | 8.8 KB，已存在 |
| M2 | `~/.codex/memories/raw_memories.md` | 原始记忆条目 | 同上 | 9.9 KB |
| M3 | `~/.codex/memories/memory_summary.md` | 摘要（首行 `v1`，含 "## User Profile"） | 同上 | 3.1 KB |
| M4 | `memories_1.sqlite` / `memories_v2_1.sqlite` → `stage1_outputs` | 每条线程的 `rollout_summary`（实测 1,727 / 1,425 字）、`raw_memory`、`usage_count` | 记忆管线（后台 job） | 各 2 行，已生成 |
| M5 | `thread_history_1.sqlite`（255 MB）/ `history.jsonl` / `session_index.jsonl` / `sessions/` / `archived_sessions/` | 全部会话原文与索引 | codex 运行时 | 454 条线程（`state_5.sqlite.threads`） |
| M6 | `.codex-global-state.json`(33.8 KB) / `state_5.sqlite`(1.4 MB) / `goals_1.sqlite` | 全局状态、目标、项目管理 | codex | 有内容 |
| M7 | `.sandbox-secrets` / `attachments/` / `computer-use/` / `browser/` / `node_repl/` | 沙箱凭据、附件、浏览器与 REPL 状态 | codex | 视使用情况 |

**关键开关**（当前值）：`memories.use_memories = false`、`[memories] generate_memories = false`、顶层 `memories = false`。
即：记忆管线现在是**关的**，但历史产物都还在盘上 —— 一旦有人把它打开（或换成默认开启的版本），`MEMORY.md` 立刻变成每轮的注入源。

## 3. 三条具体攻击路径

### 路径 A：换上游（最省事，实测可行）
```
攻击者控制/替换中转站 → 下发自己的 model-catalog（base_instructions + instructions_template）
→ codex 落盘到 ~/.codex/<provider>-model-catalog.json
→ 每轮请求的 instructions 就是攻击者的文本
```
证据：`croli-model-catalog.json` 的 `base_instructions` 与捕获里 `01.request.body` 的 instructions 同源（"You and the user share one workspace" 命中）。
现有意外缓解：主臂（`deploy=replace-base`）会把这段整体覆盖。**`insert`/`append` 下不覆盖，攻击者的系统提示词与我们的注入叠加。**

### 路径 B：往记忆层写文件
```
拿到本机（或某个会跑 codex 的 CI/容器）任意文件写权限
→ 写 ~/.codex/AGENTS.md 或 workspace/AGENTS.md
→ 或写 ~/.codex/skills/<name>/SKILL.md、~/.codex/plugins/...
→ 下一次会话，这些内容以"项目规则/技能"的身份进上下文
```
为什么比改请求更危险：**持久 + 身份更高**（AGENTS.md/SKILL.md 被模型当作可信规则），而且不需要每次都注入。

### 路径 C：让会话内容进记忆
```
引导会话产出"希望被记住"的内容（用户消息或模型回答都行）
→ 记忆管线蒸馏进 stage1_outputs.rollout_summary 与 MEMORY.md
→ 之后每次 use_memories=true 的会话都带着它
```
当前管线关闭，属**潜在**路径；一旦打开，上游回包里的注入文本（例如"以后所有任务都…"）就会沉淀成记忆。

## 4. 两个仓库的责任边界

| | helm-x | helm-a |
|---|---|---|
| 读 `AGENTS.md` / skills / plugins / MCP | **不读**（`git grep -i "agents\|skill\|mcp"` 只命中资产文本本身） | 不读 |
| 写 codex 记忆面 | **不写** | 不写 |
| 写 `config.toml` | 只改 active provider 的 `base_url` + 3 个**数值**键（`tool_output_token_limit` / `model_auto_compact_token_limit` / `_scope`） | `isocodex` 每次启动重建隔离 home 的 `config.toml`（**正面**：隔离脏配置，测试里还断言 token 不落进去） |
| 会话历史 | 不读 | `codexhome.Sessions()` 扫 `sessions/rollout-*.jsonl(.zst)` —— 只读展示；若将来做"摘要回灌"就是新入口 |
| 对 C1 的处置 | 主臂 `replace-base` 覆盖（副作用式防御）；`insert/append` 不覆盖 | 无 |
| 对 C3–C6 的处置 | 无（没有记忆面审计） | 无（隔离的是 home 配置，不是工作区 AGENTS.md） |

helm-a 里还有一份有价值的资产：`prompts/reference/codex-base-instructions-gpt-6.1-sol.md`（官方 base 原文，21,769 字符）—— 可用于和目录下发的 `base_instructions` 做比对，检测"上游是否悄悄改写系统提示词"。

## 5. 能马上做的检测（不改行为）

1. **`verify` 新增「记忆面审计」**：列出 `AGENTS.md`(全局/工作区)、`skills/**/SKILL.md`、`plugins/**` 数量、`mcp_servers` 名字、`memories/{MEMORY.md,raw_memories.md,memory_summary.md}` 的 mtime 与 sha256，以及 `use_memories` 开关状态。只读、不改。
2. **「系统提示词来源比对」**：把捕获里的 `instructions` 与 (a) `model-catalog` 的 `base_instructions`、(b) `helm-a` 的官方快照做哈希/前缀比对，输出"当前系统提示词来自：上游目录 / 主臂资产 / 客户端"。
3. **记忆层写入告警**：`MEMORY.md` / `memory_summary.md` 的 mtime 变化时，在请求记录里记一条（说明"记忆在变化"），不阻断。

## 附：不在本次范围的一处（保留记录）

`deploy=insert/append` 时客户端 `instructions` 原样进上游（实测 `ATTACKER-SYSTEM-TEXT`），以及 `clean-session` 重发原本不带 instructions（已修：重发会补回内嵌资产注入，见 `remedy.go` + `TestCleanSessionRetryKeepsInjection`）。这两条是**请求层**问题，与"记忆污染"分开记。
