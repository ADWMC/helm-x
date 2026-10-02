# 阶段 1 前置：真实 codex 请求体形态

> 目的：`PLAN-SPEC.md` §2.1 的 `RequestView` 字段假设**全部来自读 C++**（`proxy.cpp:240-440`）。
> 本文用实测替代推断。方法见文末「复现步骤」。

---

## 0. 结论摘要

用真实 codex 0.159.3 + 真实上游做了三组实测。**C++ 的三处关键假设不成立**：

| # | C++ 假设 | 出处 | 实测结论 |
|---|---|---|---|
| 1 | 存在 `"max_output_tokens"` / `"instructions"` | `proxy.cpp:463,533` | **都不存在** |
| 2 | Context Gardener 裁剪 `function_call_output` / `custom_tool_call_output` | `proxy.cpp:255` | **两个 type 都不存在 → 功能完全空转**（§5） |
| 3 | 注入 `role:"system"` 到 `input[0]` 前 | `proxy.cpp:539-542` | **成立**：上游接受，且指令确实生效（§3） |

**最重要的一条**：旧版的 Context Gardener 在当前协议下**什么都不裁**。
它按已不存在的 type 匹配，整个功能空转 —— 而这是 README 列为「核心能力 5」的功能。

**同时确认**：注入机制是**有效**的（不破坏请求，且内容被模型遵守）。
这是整个工具赖以存在的前提，实测通过。

---

## 1. 实测的请求形态

**顶层键**（按出现顺序，保序要求成立）：

```
model, stream, input, tools, reasoning, store, parallel_tool_calls,
prompt_cache_key, include, ...
```

| 字段 | 类型 | 实测值 |
|---|---|---|
| `model` | string | `gpt-5.6-terra` |
| `stream` | bool | **`true`** ← 默认就是流式 |
| `input` | array | 7 项 |
| `tools` | array | 顶层存在（另有 `additional_tools` 条目内嵌 tools） |
| `reasoning` | object | 存在 |
| `store` | bool | 存在 |
| `parallel_tool_calls` | bool | 存在 |
| `prompt_cache_key` | string | 存在 |
| `include` | array | 存在 |
| `max_output_tokens` | — | **不存在** |
| `instructions` | — | **不存在** |

### 1.1 `input[]` 的实际构成

```
[0] type=additional_tools  role=developer  (无 content，含 tools 数组)
[1] type=message           role=developer  content=[input_text]
[2] type=message           role=developer  content=[input_text, input_text]
[3] type=message           role=developer  content=[input_text]
[4] type=message           role=developer  content=[input_text]
[5] type=message           role=user       content=[input_text]
[6] type=message           role=user       content=[input_text]
```

**关键观察**：

1. **`input[0]` 不是消息**，是 `additional_tools`（工具定义载体）。
   旧版把所有东西都叫 "message"，注入时往 `input` 数组头部插 system 消息 ——
   这个行为本身可行，但**插在 `additional_tools` 之前**是否被上游接受，**未验证**。
2. **developer 角色占多数**（4/7），不是 system。旧版注入写的是 `"role":"system"`。
   真实请求里**没有 system 角色** —— 上游是否接受 system，**未验证**。
3. `input[5]`（第一个 user 条目）的文本是 `<environment_context>` 包装，
   旧版 `extract_user_message` 正是靠跳过这个标记来找真正的用户消息（`proxy.cpp:323`）——
   **这个假设成立**。
4. `input[6]` 才是真正的用户消息。

### 1.2 请求头（转发相关）

```
Accept: text/event-stream          ← 声明要流式
Content-Type: application/json
Originator: codex_exec
Session-Id: 01a0f5b4-...           ← 会话标识（N3 会话状态可用）
Thread-Id: 01a0f5b4-...            ← 同上
X-Client-Request-Id: 01a0f5b4-...
X-Codex-Window-Id: 01a0f5b4-...:0
X-Codex-Turn-Metadata: {"installation_id":"...",...}
X-Codex-Beta-Features: remote_compaction_v2
X-Openai-Internal-Codex-Responses-Lite: true
User-Agent: codex_exec/0.159.3 (Windows 10.0.26300; x86_64) ...
```

旧版转发的头白名单（`proxy.cpp:845-854`）覆盖了 Session-Id / Thread-Id /
X-Client-Request-Id / X-Codex-Window-Id / X-Codex-Turn-Metadata / User-Agent / Originator。

**未覆盖**：`X-Codex-Beta-Features`、`X-Openai-Internal-Codex-Responses-Lite`、
`Accept: text/event-stream`。**是否需要转发，未验证** —— 但 `Accept` 缺失可能导致
上游不走流式，值得在阶段 1 验证。

---

## 2. 对方案的影响

### 2.1 `PLAN-SPEC.md` §2.1 需改

`RequestView` 的字段假设要按实测调整：

```go
type RequestView struct {
    Body   []byte
    Model  string
    Stream bool          // 实测默认 true
    Inputs []InputItem
    // 以下按实测：可能不存在，需容忍缺失
    Tools     *jsonwalk.Value
    Reasoning *jsonwalk.Value
    Store     *jsonwalk.Value
    // MaxOut / Instructions 实测不存在，保留指针以便上游变化时兼容
    MaxOut       *jsonwalk.Value
    Instructions *jsonwalk.Value
}
```

### 2.2 `InputItem` 要容忍非消息类型

实测 `input[0]` 是 `additional_tools`，无 `content`。
`InputItem.Kind` 必须能表达"不是消息"，且 `Texts` 为空。

### 2.3 Context Gardener：**已删除**（见 §5）

实测裁剪目标 type 不存在，功能空转。用户裁定该功能删除，不重写。
**在抓到带工具调用的会话前不要基于旧假设实现这块 —— 现在也不必实现了。**

### 2.4 注入点：**沿用旧版做法**（已实测有效）

见 §3。往 `input[0]` 前插 `role=system` 消息，已验证被上游接受**且指令生效**。
不需要为此改设计。

---

## 3. 注入有效性实测（关键结论）

前两节说明"请求长什么样"。本节回答更要紧的问题：**注入的指令是否真的生效**。

方法：注入一条可观测指令（要求回复含标记词 `ZQ7HELMXONLINE`），
再检查响应里是否出现该标记。**"上游返回 200" 不等于 "指令被遵守"** ——
上游可以接受一个字段却忽略它，所以必须验证效果而非仅验证不报错。

| 变体 | 注入位置 | HTTP | **标记出现** |
|---|---|---|---|
| 对照 | 不注入 | 200 | **否** ✅ 对照有效 |
| B | `input[0]` **前插** `role=system`（旧版做法） | 200 | **是** ✅ |
| C | `input` **末尾追加** `role=developer` | 200 | **是** ✅ |

**结论**：

1. **U-1 已验证通过** —— 往 `input[0]` 前插 system 消息，上游正常接受。
   旧版的注入位置是**正确**的。
2. **U-2 已验证通过** —— 上游接受 `role: "system"`，尽管真实请求里从不用这个角色。
3. 两种注入位置都生效，system-first 与 developer-last 均可。**沿用旧版的 system-first**。

### 3.1 旧版实际发出的 body（对照物）

让旧版代理把上游指向记录器，抓到它真实发出的 48,657 字节：

```
input[] 共 8 项：
[0] type=message          role=system      ← 旧版注入的
[1] type=additional_tools role=developer
[2..5] type=message       role=developer
[6..7] type=message       role=user
```

与手工注入（变体 B）**结构一致** —— 旧版的注入实现是正确的。

### 3.2 一次未能复现的 502（诚实记录）

实测中出现过 2 次：经旧版代理转发返回 502（`upstream 502 (0B)`，耗时 32–35s），
而**同一份 body 直连上游返回 200**。

一度怀疑是旧版传输层缺陷。但**第 3 次经代理转发返回 200**（23s，153KB SSE 响应），
且直连亦有 20–40s 的慢响应。

**结论：无法复现，判为上游偶发超时，不记为缺陷。**
记录于此以免后人误判为已确认问题。若后续稳定复现，再立专项。

---

## 4. 仍未验证项

| # | 项 | 为何未验证 | 何时验证 |
|---|---|---|---|
| U-3 | `Accept: text/event-stream` 不转发是否有影响 | 未做对照 | 阶段 1 |
| U-4 | `function_call_output` 类型在有工具调用时是否出现 | 本次会话未用工具 | 阶段 1（需带工具的会话） |
| U-5 | 旧版转发头白名单是否遗漏必需的头 | 未逐项对照 | 阶段 1 |
| U-6 | **SSE 流式下注入是否同样生效** | 本次为验证效果强制 `stream:false` | 阶段 1 —— 真实 codex 一直是流式 |

**U-6 值得强调**：codex 默认 `stream: true`（实测），而效果验证时我把它改成了 `false`
以便拿到完整响应做比较。**流式路径下的注入有效性尚未验证**，两者在协议上略有差异。

---

## 5. Context Gardener 的裁剪目标不存在（已确证）

**这是本轮最重要的发现。**

Context Gardener 按 `type` 匹配 `function_call_output` / `custom_tool_call_output`
决定裁剪谁（`proxy.cpp:255`）。

用**真实上游**跑了一次真正调用 shell 工具的会话（codex 成功执行
`echo tool-output-marker`，7 轮往返），抓下全部请求：

| C++ 假设的 type | 实际出现次数 |
|---|---|
| `function_call_output` | **0** |
| `custom_tool_call_output` | **0** |

### 5.1 实际出现的 type（第 7 轮，48,116 字节）

```
17 × string           10 × input_text        10 × object
 8 × function          8 × message            3 × agent_message
 3 × encrypted_content 3 × number             2 × reasoning
 2 × output_text       2 × summary_text       2 × array
 2 × namespace         1 × grammar            1 × custom
 1 × boolean           1 × additional_tools
```

`input[]` 的实际构成（13 项）：

```
[0]  additional_tools   developer
[1..2] message          developer
[3..4] message          user
[5..6] message          developer
[7]  agent_message      ← C++ 从不认识的类型
[8]  message            assistant
[9]  reasoning          ← 且带 encrypted_content
[10] agent_message
[11] message            assistant
[12] reasoning
```

### 5.2 结论

1. **旧版的 Context Gardener 在本机 codex 0.159.3 上完全空转** ——
   它的两个匹配目标在这个版本的协议里都不存在，因此**从不裁剪任何东西**。
   旧版日志佐证：整个工具会话期间 `pruned` 日志**0 条**，
   且第 2 轮 `48120B -> 50997B`（增量 2877B 恰好等于注入的提示词，无裁剪）。

2. **工具结果现在以 `agent_message` / `message(assistant)` / `reasoning` 承载**，
   不再是 `function_call_output`。真正的"大块工具输出"在新协议里可能位于
   `encrypted_content`（不可读）或 `message` 的 content 中。

3. **新版的 Context Gardener 不能照搬旧逻辑** —— 而且没有重做的必要：
   该功能在当前协议下没有可靠的切入点（大块内容可能落在 `encrypted_content` 密文中），
   收益不明确。

**决定（用户裁定）：Context Gardener 删除，新版不实现。**

- `PLAN.md` M4 已标为废弃
- 阶段 2 不再包含 `contextguard`
- 旧配置里的 `context_gardener_enabled` / `context_gardener_threshold_bytes`
  字段：**读取时忽略、不报错、不再写回**（向后兼容，不破坏旧用户的配置文件）
- 前端「上下文」页移除相关的两个输入项；
  **保留**原本同页的 codex 自身压缩设置（那是另一回事，仍然生效）

---

## 6. 仍未验证项

```powershell
# 1. 构建记录型假上游
cd C:\Users\Administrator\Documents\GitHub\helm-x-wails
go build -o "$env:TEMP\helmx-capture\recordupstream.exe" ./tests/capture/recordupstream/

# 2. 起记录器
& "$env:TEMP\helmx-capture\recordupstream.exe" -addr 127.0.0.1:19000 -out "$env:TEMP\helmx-capture\bodies"

# 3. 隔离的 CODEX_HOME（绝不碰真实配置）
$ch = "$env:TEMP\helmx-capture\codexhome"
# config.toml 指向 127.0.0.1:19000/v1，wire_api = "responses"

# 4. 跑 codex
$env:CODEX_HOME = $ch
codex exec --skip-git-repo-check "say ok"
```

抓到的请求体落盘在 `bodies/`，fixture 已存入 `tests/fixtures/codex-responses-request.json`
（45,780 字节，**已确认不含凭据**：`sk-[A-Za-z0-9]{20,}` / `sk-proj-` / `nvapi-` / `sk-ant-` 均为 0 处）。

**注意**：codex 0.159.3 期望 SSE 流式响应（`Accept: text/event-stream`）。
记录器返回的是非流式 JSON，因此 codex 报 `stream disconnected before completion` 并重试 6 次。
这**不影响抓取**（6 次请求体相同），但想要 codex 正常结束一轮，记录器需返回 SSE。

---

## 5. 版本信息

| 项 | 值 |
|---|---|
| codex CLI | `codex-cli 0.159.3`（`npm i -g @openai/codex`） |
| 抓取时间 | 本次实测 |
| 请求体 | 45,780 字节 / 7 input 条目 |
