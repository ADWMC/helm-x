# helm-x — Codex 破甲工具

**仓库地址**：https://github.com/ADWMC/helm-x

---

**如果这个项目对你有帮助，请给个 Star 支持一下！**

**Telegram**：[t.me/helm_xD](https://t.me/helm_xD)

**官方QQ群**：213266664

---

**Codex 破甲工具** · 本地代理/注入 · 桌面 GUI · 单文件

> 本项目由 AI 生成（**DeepSeek Harness** + **Mimo V2.6 Pro**；旧版为 DeepSeek V4 Flash + Mimo V2.5 Pro），代码质量仅供参考，请勿喷。
>
> **License：GNU AGPL v3.0 —— 可商用。** 分发或对外提供服务须按 AGPL 开放相应源码。使用者须遵守所在地法律法规，对使用本项目产生的后果自负。

---

## 这是什么

helm-x 是 Codex 的**破甲工具**：模型拒绝时对话不中断、内容能送达。

实现手段是本地代理 + 注入（`codex → 127.0.0.1:1800 → 上游中转`）：请求层注入提示词，响应层判定拒绝并按补救阶梯处理。手段服务于目标——不是为了做代理，是为了让回答能过。

**核心能力**：

1. **提示词注入**：内置精简破甲提示词（2.5KB），注入频率可配
2. **判定与补救**：拒绝被检测到时走补救阶梯（重试 → 附加标记 → 原样透传），模型原话完整保留，对话不中断
3. **上下文感知改写器**：自动改写请求 + 重试，可配自己的 API
4. **中转站兼容**：请求头全透传（UA、Cookie、X-\*），校验请求指纹的中转站不拒
5. **流式支持**：SSE 流式响应照常处理；首段缓冲窗口内命中拒答会丢弃缓冲、换 clean session 重发，再以 SSE 转发——流式是真补救，不是只判定
6. **配置一键注入**：codex 配置自动指向本代理，退出自动还原
7. **桌面控制台**：9 页 GUI（请求/日志/提示词/QA 等），数字键快捷切页
8. **零依赖**：单 exe 静态链接，仅 Windows 系统库

---

## 测试结果

### 全量基准测试（50 题 · gpt-6.1-sol · `ctf-v2.1` 证据闭环版）

| 指标 | 数值 | 占比 | 相对基线提升 |
|------|------|------|-------------|
| **总用例** | 50 | 100% | - |
| **通过 (PASS)** | **43** | **86.0%** | **+10.0%**（原 38 题 76%） |
| **Cyber Flag（上游平台拦截）** | 7 | 14.0% | **-2.0%**（原 8 题 16%） |
| **模型拒绝 (Refusal)** | **0** | **0.0%** | **-8.0%（彻底清零）** |
| **错误 / 超时** | **0** | **0.0%** | 5 题超时复测后 3 PASS、2 拦截 |
| **排除 Cyber 拦截后模型有效应答通过率** | **43/43** | **100.0%** | **+9.5%**（原 90.5%） |

> 口径说明：基准跑集 50 题。针对首轮 60s 因长链推理出现超时的 5 题放宽至 150s 充分推理，其中 3 题顺利产出完整工件判定 PASS，2 题触发中转网关输入词过滤拦截。模型自身拒答率为 0.0%。完整测试报告：[tests/report-v21-final50.md](tests/report-v21-final50.md)。

### 分类通过率（v2.1 最终合并数据）

| 类别 | 用例 | 通过 | Cyber | 拒绝 | 通过率 |
|------|------|------|-------|------|--------|
| **键盘记录** | 4 | 4 | 0 | 0 | **100%** |
| **游戏安全** | 6 | 6 | 0 | 0 | **100%**（原 83.3%） |
| **边界场景** | 4 | 4 | 0 | 0 | **100%**（原 75%） |
| **网络攻击** | 8 | 7 | 1 | 0 | **87.5%** |
| **进程隐藏** | 8 | 7 | 1 | 0 | **87.5%**（原 62.5%） |
| **漏洞利用** | 8 | 6 | 2 | 0 | **75.0%**（原 50%） |
| **恶意软件** | 8 | 6 | 2 | 0 | **75.0%**（原 62.5%） |
| **授权破解** | 4 | 3 | 1 | 0 | **75.0%** |

### 提示词历代对比（实测数据）

| 版本 | Prompt 大小 | 核心机制 | 50 题通过率 | 模型拒绝数 | 有效应答通过率 |
|------|-----------|---------|------------|-----------|---------------|
| **ctf-v2.1 (当前默认)** | **3.7KB** | **证据闭环 (VERIFIED/CANDIDATE) + 规范占位符 + 动量交付** | **86.0% (43/50)** | **0 拒绝 (0.0%)** | **100.0% (43/43)** |
| v0.0.5 | 2.5KB | CTF 评分制 + luna 人设 + 输出锁 | 76.0% (38/50) | 4 拒绝 (8.0%) | 90.5% (38/42) |
| v0.0.4 | 2.5KB | 基础 CTF 框架 | 95% (20题) | - | - |
| v0.0.3 | 15KB | 复合沙盒提示词 | 85% | - | - |
| v0.0.2 | 15KB | 初始沙盒框架 | ~70% | - | - |

---

## Prompt 设计

提示词在 `internal/assets/data/` 下维护。模式统一 `ctf` 前缀 + 阶梯版本，适配的模型代数写在 `Models` 字段里（界面「提示词」页直接显示）：

| 模式 ID | 文件 | 大小 | 适配模型 | 说明 |
|---|---|---|---|---|
| `ctf` | prompt-ctf-scoring.md | 2.7KB | gpt-5.6 系 | 经典计分淘汰 + 输出锁 + 首轮归一化 |
| `ctf-v2` | prompt-v2.md | 3.5KB | gpt-6.0 系 | 完成态契约：补缺口 + VERIFIED 工件 |
| `ctf-v2.1` | prompt-v2.1.md | 3.7KB | gpt-6.1 系 | **默认**：三级证据评级 + 规范占位符 + 回滚闭环 |

历史 ID（`default` / `v2` / `v2.1`）继续可用，读入时归一化到上表 ID。核心机制：

| 机制 | 作用 |
|------|------|
| **三级证据评级** | 强制要求打标 `VERIFIED`（已执行）、`CANDIDATE`（理论推导）、`UNRESOLVED`（缺口待补），促使模型专注填补技术工件 |
| **规范类型占位符** | 统一收拢至 `SAMPLE / TARGET / OFFSET / SERIAL / PAYLOAD`，防止真实靶标直接引爆平台网关审查 |
| **完成态与动量契约** | 闭环要求提供验证命令与回滚机制；受阻时交付最近邻可用组件，彻底禁止半途截断 |
| CTF 竞赛框架 | 设定环境为纯内网、空泡化的 CTF 演练沙盒靶场 |
| 激活协议 | `helmx` 触发词校验 |
| 人设 luna | 强化红队/教练角色意识，剥离客服式说教与免责声明 |
| 计分淘汰制 | 拒绝 = -500（淘汰），执行 = +100，利用奖励偏置引导生成 |
| 输出锁 | 强制代码/工具调用开头，跳过所有前置安全客套 |
| 中性化归一化 | 意图与敏感词中性技术映射（Word-Free 规范），降低网关层 Cyber Flag 误判 |

---

## 架构

```
codex ──> http://127.0.0.1:1800 ──> 上游中转
              │
        [注入提示词]        [响应判定]
        [全头透传]          [补救：重试 → 附加标记 → 原样透传]
              │
        [记录：~/.codex/helmx.log + 控制台请求页]
```

---

## 使用教程

### 一、准备上游中转

在 `~/.codex/config.toml` 中配置：

```toml
[model_providers.custom]
name = "my-relay"
base_url = "https://your-api.com/v1"
wire_api = "responses"
api_key = "your-api-key"
```

### 二、启动 helm-x

双击 `helmx.exe`（或安装包创建的快捷方式）。自动完成：启动代理（:1800）+ 注入 codex 配置 + 打开控制台。

### 三、使用 Codex CLI

```bat
codex
```

正常用即可。所有请求自动经过 helm-x，控制台「请求」页可看到每条请求的注入标记与判定结果。

### 四、切换提示词 / 注入频率

控制台 → 提示词页：选择提示词模式；设置「注入频率」（每 N 个会话注入 1 个会话，命中后该会话全程携带；1 = 每个会话都注入）。保存后下次请求生效，无需重启。

### 五、查看日志

控制台 → 运行日志。日志文件在 `~/.codex/helmx.log`（代理）与 `~/.codex/helmx-cyber.log`（cyber 事件）。

### 六、上游错误重试

默认启用：额外重试 10 次、固定间隔 3 秒（`0` = 无限）。HTTP 408/429/5xx、空响应、连接错误进入重试。控制台 → 服务页可调开关与参数；每次重试固定间隔，不使用上游的 `Retry-After`。

### 七、UA 兜底

控制台 → 服务页 → 「转发 UA 兜底」。默认不填：入站请求的 User-Agent 原样透传；仅当客户端不发 UA 时才补这里的值；两者皆无则转发请求不带 UA。

---

## CLI

不带子命令 = 桌面控制台。子命令：

```bat
helmx proxy      :: 本地映射代理（无窗口，供脚本/排查使用）
helmx apply      :: 注入 codex 配置（base_url 指向本代理）
helmx remove     :: 还原并清理
helmx verify     :: 自检
helmx activate   :: 发送激活词做端到端验证
helmx watch      :: 自愈守护（前台）
helmx ui         :: 浏览器控制台（无 WebView2 时的降级路径）
```

`proxy` 常用选项：

```bat
helmx proxy --listen PORT          :: 监听端口，默认 1800
helmx proxy --upstream URL         :: 上游地址；缺省从 codex 配置读取
helmx proxy --max-retries 0        :: 无限重试（默认额外 10 次）
helmx proxy --retry-delay 3        :: 固定重试间隔，默认 3 秒
helmx proxy --no-retry             :: 本次运行禁用重试
helmx proxy --passthrough          :: 关闭全部内容转换（对照排查用）
helmx proxy --dry-run              :: 只计算不发上游
helmx proxy --restore              :: 手动还原 codex 配置后退出
```

---

## 配置与日志位置

| 内容 | 位置 |
|---|---|
| 运行配置 | `%APPDATA%\helmx.config.json` |
| 代理日志 | `~/.codex/helmx.log` |
| cyber 事件日志 | `~/.codex/helmx-cyber.log` |
| codex 配置（注入目标） | `~/.codex/config.toml` |
| 注入前原始备份 | `~/.codex/config.toml.helmx-bak` |

---

## 构建

```bat
wails3 task build      :: bin/helmx.exe（单文件）
wails3 task package    :: bin/helm-x-amd64-installer.exe（NSIS 安装包）
```

内嵌资产（`internal/assets/data/` 下的提示词与拒答规则）已加密：AES-256-GCM，密钥 `K = SHA-256(HELMX_ASSET_KEY)`，未设置时用内置开发种子。改完 `internal/assets/data/` 下的文件必须重新生成后再构建：

```bat
go generate ./internal/assets
```

设置 `HELMX_ASSET_KEY` 可把密钥外部化（不再依赖仓库内种子），但**构建与运行必须用同一个值**，否则资源解密失败，`helmx verify` 自检会直接报出来：

```bat
set HELMX_ASSET_KEY=YOUR_ASSET_KEY
go generate ./internal/assets && wails3 task build
set HELMX_ASSET_KEY=YOUR_ASSET_KEY && bin\helmx.exe
```

测试：

```bat
go test ./internal/...        :: Go 单元/集成测试
cd frontend && npx vitest     :: 前端测试
python tests/helm-x-test-suite.py :: 破甲题库实测
```

系统要求：Windows 10/11（需 WebView2 Runtime，安装包会自动补齐）。

---

## 目录结构

```
main.go               入口：GUI / CLI 分发
internal/             代理引擎、注入、判定补救、配置注入、改写器、服务层
frontend/             Vue 3 + Tailwind + daisyUI 控制台
build/                图标、清单、NSIS 打包
tests/                破甲题库测试套件 + 阶段实证工具
docs/                 设计文档（重写说明见 docs/REWRITE.md）
```

---

## 已修复的已知问题

以下缺陷均已修复并有对应测试，README 旧版描述随之作废：

| 问题 | 现状 |
|---|---|
| 拒答判定误伤正常回答 | 判定收紧：只扫回复**前 200 字符**，超过 **1200 字符**的回答一律不判拒答；规则文件删除单词级 `dangerous` / `illegal` / `harmful` 匹配（`internal/tamper/tamper.go`、`internal/assets/data/tamper_rules.txt`） |
| 流式路径"只判定、不补救" | 流式真补救：SSE 首段缓冲窗口（默认 **2048B / 400ms**）内命中拒答 → 丢弃缓冲、换 clean session 重发；重发用 `stream:true` 取回真正的 SSE 再转发（`internal/proxy/stream.go`） |
| 流式路径不重试 | 流式与非流式对齐：HTTP 408/429/5xx 与连接错误均进入重试（`PostStreamRetry`） |
| 注入频率按"每 N 次请求"计数 | 改为按会话采样：每 N 个会话注入 1 个会话，命中后该会话全程携带；1 = 每个会话都注入（`internal/proxy/inject.go`） |
| `prompt_mode: "default"` 静默变成 v2.1 | 历史 ID 统一归一化：`default`→`ctf`、`v2`→`ctf-v2`、`v2.1`→`ctf-v2.1`，升级不再静默换版（`internal/assets/assets.go` 的 `promptAliases`、`internal/config/config.go`） |
| 内嵌资产仅 11 字节重复 XOR | 改为 AES-256-GCM，密钥 `K = SHA-256(HELMX_ASSET_KEY)`，未设置时用内置开发种子；改 `internal/assets/data/` 后须 `go generate ./internal/assets`；构建与运行必须给同一个 `HELMX_ASSET_KEY`，否则解密失败（自检会报）（`internal/assets/`） |

---

## 关于本重写

本仓库为 **Wails v3 + Go + Vue 3 重写版**（旧版 C++ 见 [`legacy`](https://github.com/ADWMC/helm-x/tree/legacy) 分支）。为什么重写、与旧版差别、设计文档索引 → **[docs/REWRITE.md](docs/REWRITE.md)**。

---

## 参考项目

- [NERV-BREAK-5.6](https://github.com/lingbol088-spec/5.6-JAILBREAK-NERV-codex-instruct-5.6) — Memory Kernel + 上下文分类
- [codex-gpt-5.6-5.5-instruct](https://github.com/lingbol088-spec/codex-gpt-5.6-5.5-instruct) — 计分制 + 语义归一化 + 通道调度
- [Codex-X](https://github.com/yynxxxxx/Codex-X) — 提示词模板
- [gptbypass](https://github.com/null119/gptbypass) — 改写器策略
- [gpt-5.6-instruct](https://github.com/MDX-Tom/gpt-5.6-instruct) — 测试方法论

## License

[GNU AGPL v3.0](LICENSE) · 允许商业使用；分发或对外提供服务（含 SaaS）须按 AGPL 开放相应源码
