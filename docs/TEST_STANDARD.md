# helm-x 测试标准

## 验收目标

- 只读写 Claude Code 的 `settings.json`，仅更新 `env.ANTHROPIC_BASE_URL`。
- 保留其他 Claude Code 设置、凭据和用户文件。
- 不读取或修改 `.codex`。
- WebUI、改写器和上下文设置只使用 `%APPDATA%\\helmx-claudecode.config.json`，不读取通用 `helmx.config.json`。
- 代理接受 Anthropic Messages API 请求，并在 `system` 首项注入提示词。
- 保留 Claude Code 的真实 User-Agent，并转发 `anthropic-*`、`x-claude-*`、`x-stainless-*`、`x-app` 和认证请求头。
- 退出、恢复和卸载路径可还原原始 `settings.json`。

## 自动化测试

```powershell
python -m unittest tests.test_config tests.test_ui tests.test_embed -v
```

覆盖范围：

| 文件 | 覆盖内容 |
|---|---|
| `tests/test_config.py` | apply、verify、remove、代理备份恢复、无效 JSON、保留其他设置、`.codex` 不变 |
| `tests/test_ui.py` | Anthropic 请求头与 UA 转发、system 注入、上下文裁剪、结构化错误、WebUI 配置保存 |
| `tests/test_embed.py` | 嵌入资源默认值和离线 FAQ 完整性 |

## 构建验证

```powershell
cmake -S . -B build-check -G "MinGW Makefiles"
cmake --build build-check -j
```

构建后再次运行自动化测试，确保测试使用最新的 `build-check/helmx.exe`。

## Claude Code 实链路验证

1. 使用临时 `CLAUDE_CONFIG_DIR`，避免影响日常配置。
2. 启动一个本地捕获服务作为上游。
3. 启动 `helmx proxy --upstream <fixture-url>`。
4. 运行 `claude -p helmx --output-format text`。
5. 检查捕获结果：
   - 路径为 `/v1/messages`，可带查询参数。
   - User-Agent 与 Claude Code 发出的值一致。
   - Anthropic 和 Claude Code 客户端请求头已转发。
   - 请求体仍包含原始 `messages`、`tools`、`thinking` 等字段。
   - 注入提示词位于 `system` 数组首项。
6. 检查临时目录中的 `.codex` 标记文件未发生变化。

实链路测试依赖本机已安装并配置 Claude Code，因此不作为普通单元测试的前置条件。

## 结果报告

完成时记录实际执行的构建和测试命令、退出结果、未执行项及原因。历史模型测试报告仅作为旧版本记录，不代表当前 Claude Code 分支的验收结果。
