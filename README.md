# helm-x - Claude Code 本地映射工具

**Claude Code 本地代理控制工具**，C++17 单二进制，Windows x64。

[English](README_EN.md) | [更新日志](CHANGELOG.md) | [常见问题](docs/FAQ.md)

## 工作方式

helm-x 在 Claude Code 与 Anthropic API 中转之间运行本地代理：

```text
Claude Code -> http://127.0.0.1:1800 -> Anthropic API 中转
```

代理处理 Anthropic Messages API (`/v1/messages`) 请求，注入嵌入式 system 指令，并提供请求改写、响应检测、Context Gardener 和 WebUI。

helm-x 只读写 Claude Code 配置：

```text
~/.claude/settings.json
~/.claude/settings.json.helmx-bak
~/.claude/settings.json.helmx-proxy-bak
~/.claude/helmx.log
```

程序不读取或修改 `~/.codex`。

## 环境要求

- Windows 10/11 x64
- Claude Code 2.x
- Claude Code 已配置可用的 `ANTHROPIC_BASE_URL` 和凭据
- 构建需要 CMake 3.16+ 与支持 C++17 的 MinGW-w64 或 Visual Studio

Claude Code 配置示例：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://your-relay.example",
    "ANTHROPIC_AUTH_TOKEN": "your-token"
  }
}
```

## 使用

### WebUI

双击 `helmx.exe`：

1. 启动本地代理 `127.0.0.1:1800`
2. 将 `ANTHROPIC_BASE_URL` 临时指向本地代理
3. 打开 `http://127.0.0.1:8090`
4. 在新终端运行 `claude`

关闭 helm-x 后，Claude Code 的原始 `ANTHROPIC_BASE_URL` 会从代理备份恢复。

### 命令行

```powershell
helmx apply
helmx proxy --listen 1800
claude
```

常用命令：

```text
helmx apply          配置 Claude Code 使用 127.0.0.1:1800
helmx verify         检查 settings.json、备份和嵌入式资源
helmx verify --e2e   额外执行 claude -p "helmx"
helmx activate       发送激活词
helmx proxy          启动 Anthropic Messages API 代理
helmx proxy --restore
                     恢复代理启动前的 settings.json
helmx ui             启动 WebUI
helmx watch          定期检查并恢复代理配置
helmx remove         恢复 apply 前的 settings.json
```

指定 Claude Code 配置目录：

```powershell
$env:CLAUDE_CONFIG_DIR = "D:\claude-profile"
helmx apply
```

## 配置与恢复

`apply` 首次运行时完整备份 `settings.json`，随后只更新：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:1800"
  }
}
```

其他 Claude Code 设置和 `env` 变量保持原值。无效 JSON 会被拒绝，原文件保持不变。

`proxy` 启动时使用 `settings.json.helmx-proxy-bak` 保存中转地址；正常退出或执行 `proxy --restore` 时恢复。`remove` 使用 `settings.json.helmx-bak` 恢复 `apply` 前的完整配置。

## 构建与测试

```powershell
cmake -S . -B build -G "MinGW Makefiles"
cmake --build build -j
$env:HELMX_TEST_BIN = (Resolve-Path "build\helmx.exe")
python -m unittest tests.test_config tests.test_ui tests.test_embed -v
```

## 许可证

[MIT](LICENSE)
