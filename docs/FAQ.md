# FAQ

### helm-x 做什么？

它是 Claude Code 的本地 Anthropic Messages API 代理。链路为 `Claude Code -> 127.0.0.1:1800 -> API 中转`，代理负责 system 指令注入、上下文裁剪和响应处理。

### 需要什么环境？

Windows 10/11、Claude Code 2.x，以及 Claude Code 中已经可用的 Anthropic API 中转配置。运行发布版无需 Python；Python 只用于测试和重新生成嵌入资源。

### 上游地址从哪里读取？

从 `~/.claude/settings.json` 的 `env.ANTHROPIC_BASE_URL` 读取。设置 `CLAUDE_CONFIG_DIR` 可指定其他配置目录。

### helm-x 会修改哪些配置？

只把 `env.ANTHROPIC_BASE_URL` 临时改为 `http://127.0.0.1:1800`。首次 `apply` 和每次代理启动都会创建对应备份；其他 Claude Code 设置保持原值。

### 如何恢复？

```powershell
helmx proxy --restore
helmx remove
```

第一条恢复代理启动前的配置，第二条恢复 `apply` 前的完整 `settings.json`。

### Claude Code 连接超时？

确认 helm-x 代理正在监听 `127.0.0.1:1800`，再检查 WebUI 的上游地址。代理未运行时执行 `helmx proxy --restore`，让 Claude Code 重新直连原中转。

### 返回 401 或 403？

检查 Claude Code 配置中的 `ANTHROPIC_AUTH_TOKEN` 或 `ANTHROPIC_API_KEY`。helm-x 会原样转发 `Authorization`、`x-api-key`、`anthropic-version` 和 `anthropic-beta` 请求头。

### 日志在哪里？

`~/.claude/helmx.log` 和 `~/.claude/helmx-cyber.log`。日志不会记录完整凭据。

### 配置 JSON 损坏时会怎样？

helm-x 会拒绝写入并保留原文件。修复 `settings.json` 后重新执行 `helmx apply`。
