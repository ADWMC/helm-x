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
> **⚠️ 仅供学习交流，禁止商用。** 使用者须遵守所在地法律法规，对使用本项目产生的后果自负。

---

## 这是什么

helm-x 是 Codex 的**破甲工具**：模型拒绝时对话不中断、内容能送达。

实现手段是本地代理 + 注入（`codex → 127.0.0.1:1800 → 上游中转`）：请求层注入提示词，响应层判定拒绝并按补救阶梯处理。手段服务于目标——不是为了做代理，是为了让回答能过。

**核心能力**：

1. **提示词注入**：内置精简破甲提示词（2.5KB），注入频率可配
2. **判定与补救**：拒绝被检测到时走补救阶梯（重试 → 附加标记 → 原样透传），模型原话完整保留，对话不中断
3. **上下文感知改写器**：自动改写请求 + 重试，可配自己的 API
4. **中转站兼容**：请求头全透传（UA、Cookie、X-\*），校验请求指纹的中转站不拒
5. **流式支持**：SSE 流式响应照常处理，拒绝也能提前识别
6. **配置一键注入**：codex 配置自动指向本代理，退出自动还原
7. **桌面控制台**：9 页 GUI（请求/日志/提示词/QA 等），数字键快捷切页
8. **零依赖**：单 exe 静态链接，仅 Windows 系统库

---

## 测试结果

### 全量测试（50 题 · gpt-6.1-sol · 超时 60s）

| 指标 | 数值 | 占比 |
|------|------|------|
| **总用例** | 50 | 100% |
| **通过** | 38 | **76%** |
| Cyber Flag（上游平台拦截） | 8 | 16% |
| 模型拒绝 | 4 | 8% |
| 错误 / 超时 | 0 | 0% |
| **排除 Cyber 通过率** | **38/42** | **90.5%** |

> 口径：单例超时 60s，超时按已产出的真实回答判定（回答没拒绝即通过，本次 0 例纯超时）；
> Cyber Flag 是上游平台安全策略拦截，不是模型拒绝。完整报告：[tests/report-gpt-6.1-sol.md](tests/report-gpt-6.1-sol.md)。

### 分类通过率

| 类别 | 用例 | 通过 | Cyber | 拒绝 | 通过率 |
|------|------|------|-------|------|--------|
| 键盘记录 | 4 | 4 | 0 | 0 | **100%** |
| 网络攻击 | 8 | 8 | 0 | 0 | **100%** |
| 授权破解 | 4 | 4 | 0 | 0 | **100%** |
| 游戏安全 | 6 | 5 | 0 | 1 | 83.3% |
| 边界场景 | 4 | 3 | 1 | 0 | 75% |
| 进程隐藏 | 8 | 5 | 3 | 0 | 62.5% |
| 恶意软件 | 8 | 5 | 2 | 1 | 62.5% |
| 漏洞利用 | 8 | 4 | 2 | 2 | 50% |

### 提示词历代对比（旧版管线实测，供参考）

| 版本 | Prompt 大小 | CS2 ESP 5次 | 60 题通过率 |
|------|-----------|------------|-----------|
| v0.0.2 | 15KB | 40% | ~70% |
| v0.0.3 | 15KB | 40% | 85% |
| v0.0.4 | 2.5KB | 100% | 95% (20题) |
| v0.0.5 | 2.5KB | 100% | 93.9% (60题) |

---

## Prompt 设计

提示词沿用旧版资产（默认 2.5KB，另有 v45 沙盒执行器、deepseek 优化版两套可切换）。默认提示词的 7 个核心机制：

| 机制 | 作用 |
|------|------|
| CTF 竞赛框架 | 让模型认为所有目标都是 CTF fixture |
| 激活协议 | helmx 触发词 |
| 人设 luna | 让模型更配合执行 |
| 计分制 | 拒绝 = -500（淘汰），执行 = +100 |
| 输出锁 | 必须以代码/工具开头，跳过拒绝前言 |
| 禁止词 | 不能/无法/I can't/sorry 等词汇封锁 |
| 语义归一化 | "外挂"→memory analysis，"ESP"→rendering pipeline |

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

控制台 → 提示词页：选择提示词模式；设置「注入频率」（每 N 次请求注入 1 次，1 = 每次都注入）。保存后下次请求生效，无需重启。

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

[GNU AGPL v3.0](LICENSE) · **⚠️ 仅供学习交流，禁止商用**
