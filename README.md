# helm-x — Codex 代理/注入工具（Wails v3 重写版）

**仓库地址**：https://github.com/ADWMC/helm-x

---

**如果这个项目对你有帮助，请给个 Star 支持一下！**

**Telegram**：[t.me/helm_xD](https://t.me/helm_xD)

**官方QQ群**：213266664

---

**Codex CLI 本地映射层** · 桌面 GUI · 单文件 · Go + Vue 3

> 本项目由 AI 生成，代码质量仅供参考。
>
> **⚠️ 仅供学习交流，禁止商用。** 使用者须遵守所在地法律法规，对使用本项目产生的后果自负。

---

## 这是什么

helm-x 是 Codex CLI 的本地映射层：`codex → 127.0.0.1:1800 → 上游中转`。请求经本地代理转发时注入自定义指令，响应层检测拒绝并按补救阶梯处理——对话不中断，**模型原话不丢失**。

本项目是旧版（C++，见 [`legacy`](https://github.com/ADWMC/helm-x/tree/legacy) 分支）的**重写**：机制沿用旧版思路，实现按实测结论重新设计（做了 Phase A 实证：旧版四个行为推断里两个被实测推翻，详见 `docs/FINDINGS*.md`）。

**核心能力**：

1. **提示词注入**：以 `system` 角色插入 `input[0]`——该位置实测被上游接受且指令生效（模型回复出现指定标记词）。字节级改写，不重排请求、不动未涉及字段
2. **注入频率可配**：每 N 次请求注入 1 次，按会话（Session-Id）各自计数，注入失败不占名额
3. **判定与补救**：响应六态判定（正常/已改写/已重建会话/上游失败/格式异常/未判定）→ 补救阶梯 `重试 → 附加标记 → 原样透传`。与旧版的关键差别：**附加**而非替换，绝不删除模型原有输出
4. **全头透传**：除逐跳头外全部原样转发（含 User-Agent、Cookie、Authorization、X-\*）。中转站看到的请求指纹与 codex 直连一致，不惧校验 UA/请求头的中转站
5. **流式支持**：SSE 首段缓冲窗口（2048 字节 / 400ms），流式响应里提前识别拒绝
6. **配置注入与还原**：行级 TOML 模型改写 codex 的 `config.toml`，退出时字节精确还原（改动前自动备份）
7. **改写器**：上下文感知改写 + 重试，可配自己的 API
8. **桌面控制台**：9 页 GUI（概览 / 请求 / 自检 / 服务 / 提示词 / 上下文 / 改写器 / QA 帮助 / 运行日志），数字键 `1-9` 快捷切页

---

## 与旧版的差别

| 项 | 旧版（`legacy` 分支，C++） | 本版（Wails v3 + Go + Vue 3） |
|---|---|---|
| 界面 | 浏览器 WebUI（:8090） | 原生桌面窗口（WebView2） |
| 拒绝处理 | **替换**模型原话，只留标记 | **附加**标记，原话完整保留 |
| Context Gardener | 有 | **删除** —— 实测裁剪目标在当前协议里不存在，功能空转 |
| 请求头 | 挑选转发 | 逐跳头之外全透传 |
| 注入频率 | 每个请求注入 | 可配（每 N 次注入 1 次，按会话计数） |
| 打包 | 单 exe | 单 exe + NSIS 安装包 |

---

## 架构

```
codex ──> http://127.0.0.1:1800 ──> 上游中转
              │
        [字节级解析 jsonwalk]   [注入提示词 → input[0]]
        [全头透传（UA/Cookie/X-*）]  [响应判定：六态]
              │
        [补救：retry → 附加标记 → 原样透传]
              │
        [记录：~/.codex/helmx.log + 控制台请求页]
```

提示词内置三套（默认 / v45 沙盒执行器 / deepseek 优化版），在控制台「提示词」页切换，下次请求即生效。

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

正常用即可。所有请求自动经过 helm-x：注入提示词、全头透传、判定与补救。控制台「请求」页可看到每条请求的注入标记与判定结果。

### 四、切换提示词 / 注入频率

控制台 → 提示词页：选择提示词模式；设置「注入频率」（每 N 次请求注入 1 次，1 = 每次都注入）。保存后下次请求生效，无需重启。

### 五、查看日志

控制台 → 运行日志。日志文件在 `~/.codex/helmx.log`（代理）与 `~/.codex/helmx-cyber.log`（cyber 事件）。

### 六、上游错误重试

默认启用：额外重试 10 次、固定间隔 3 秒（`0` = 无限）。HTTP 408/429/5xx、空响应、连接错误进入重试。控制台 → 服务页可调开关与参数；每次重试固定间隔，不使用上游的 `Retry-After`。

### 七、UA 兜底

控制台 → 服务页 → 「转发 UA 兜底」。默认不填：入站请求的 User-Agent 原样透传；仅当客户端不发 UA 时才补这里的值；两者皆无则转发请求不带 UA（不会出现 `Go-http-client` 之类中转站不认识的 UA）。

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
| codex 配置（注入目标） | `~/.codex\config.toml` |
| 注入前原始备份 | `~/.codex/config.toml.helmx-bak` |

---

## 目录结构

```
main.go               入口：GUI / CLI 分发
internal/
  proxy/              代理引擎：解析 → 注入 → 转发 → 判定 → 补救
  protocol/jsonwalk   字节级 JSON 读写（不重序列化）
  protocol/sse        SSE 分帧
  codexcfg/           行级 TOML 模型（字节精确还原 config.toml）
  tamper/             拒绝句式规则引擎（附加标记，不删原文）
  rewriter/           改写器
  svc/                服务层（Wails 绑定面）
  cli/                CLI 子命令
  runtime/            组装层
frontend/             Vue 3 + Tailwind + daisyUI 控制台
build/                图标、清单、NSIS 打包
docs/                 PLAN / PLAN-SPEC / DESIGN / FINDINGS / ADR
tests/                阶段实证工具与 fixtures
```

---

## 构建

```bat
wails3 task build      :: bin/helmx.exe（单文件）
wails3 task package    :: bin/helm-x-amd64-installer.exe（NSIS 安装包）
```

测试：

```bat
go test ./internal/...       :: Go 单元/集成测试
cd frontend && npx vitest    :: 前端测试
```

系统要求：Windows 10/11（需 WebView2 Runtime，安装包会自动补齐）。

---

## 参考项目

- [NERV-BREAK-5.6](https://github.com/lingbol088-spec/5.6-JAILBREAK-NERV-codex-instruct-5.6) — Memory Kernel + 上下文分类
- [codex-gpt-5.6-5.5-instruct](https://github.com/lingbol088-spec/codex-gpt-5.6-5.5-instruct) — 计分制 + 语义归一化 + 通道调度
- [Codex-X](https://github.com/yynxxxxx/Codex-X) — 提示词模板
- [gptbypass](https://github.com/null119/gptbypass) — 改写器策略
- [gpt-5.6-instruct](https://github.com/MDX-Tom/gpt-5.6-instruct) — 测试方法论

## License

[GNU AGPL v3.0](LICENSE) · **⚠️ 仅供学习交流，禁止商用**
