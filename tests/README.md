# helm-x 测试

当前自动化测试覆盖 Claude Code 配置、独立的 `helmx-claudecode.config.json`、Anthropic Messages API 代理行为、WebUI 和嵌入资源。

```powershell
python -m unittest tests.test_config tests.test_ui tests.test_embed -v
```

默认测试二进制为 `build-check/helmx.exe`。需要指定其他构建时：

```powershell
$env:HELMX_TEST_BIN = "C:\path\to\helmx.exe"
python -m unittest tests.test_config tests.test_ui tests.test_embed -v
```

| 文件 | 覆盖内容 |
|---|---|
| `test_config.py` | `settings.json` 的 apply、verify、remove、备份恢复、无效 JSON 和 `.codex` 隔离 |
| `test_ui.py` | 请求头与 UA 转发、system 注入、上下文裁剪、错误响应和 WebUI 配置 |
| `test_embed.py` | 可选嵌入配置默认值和离线 FAQ |

旧版矩阵脚本和报告保留为历史记录，不属于当前回归测试入口。
