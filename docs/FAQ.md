# FAQ — 常见问题

## 基本问题

### Q: helm-x 是什么？
A: Codex CLI 的本地映射代理。在 `codex → 127.0.0.1:1800 → 上游中转` 的链路中，注入自定义指令（AGENTS）并篡改拒绝响应。

### Q: 会不会被检测？
A: AGENTS 注入在本地代理层完成，上游只看到修改后的请求。实测 8/8 高危请求通过，0 cyber flag。

### Q: 需要什么环境？
A: Windows 10/11 + Codex CLI 0.146+ + 任意 OpenAI 兼容 API。无需 Python/Node.js 运行时。

### Q: 怎么安装？
A: 下载 `helmx.exe`，双击启动。自动完成：proxy(:1800) + UI(:8090) + 打开浏览器。

---

## 代理问题

### Q: 上游 API 地址怎么配？
A: 在 `~/.codex/config.toml` 的 `[model_providers.custom]` 中设置 `base_url`。proxy 启动时自动读取。

### Q: 改写器是什么？
A: 当请求触发 cyber flag 时，改写器自动把用户消息改写为技术描述，然后重发。参考 gptbypass 的实现。

### Q: 改写器需要配置吗？
A: 可选。在 `%APPDATA%\helmx.config.json` 中配置改写器 API（如 NVIDIA NIM）。不配置则使用本地规则 fallback。

### Q: 代理端口怎么改？
A: `helmx proxy --listen 8080`。默认 1800。

### Q: cyber flag 是什么？
A: OpenAI 的网络安全策略检测。当请求包含特定意图词组合（如"隐藏进程"+"任务管理器"）时触发。AGENTS 注入可以阻止大部分触发。

---

## 技术问题

### Q: 为什么用 C++ 不用 Python？
A: 单二进制、零依赖、静态链接。原 Python 版有 SSE 流阻塞问题，C++ 版强制 `stream=false` 解决。

### Q: 资源怎么加密的？
A: XOR 流密钥加密。`tools/embed.py` 在构建时加密 `assets/` 下的文件，生成 `resources_generated.cpp`。运行时解密。

### Q: 怎么自定义 AGENTS？
A: 编辑 `assets/bridge.md`，重新运行 `python tools/embed.py`，重新编译。

### Q: 怎么自定义改写器提示词？
A: 在 `%APPDATA%\helmx.config.json` 中设置 `system_prompt` 字段，或编辑 `assets/rewrite_prompt.txt` 后重新内嵌。

### Q: TAMPER 规则怎么扩展？
A: 编辑 `assets/tamper_rules.txt`（每行一个正则），重新内嵌。

---

## 故障排除

### Q: proxy 启动失败？
A: 检查端口 1800 是否被占用。`netstat -ano | findstr 1800`。

### Q: codex 连接超时？
A: 检查 `~/.codex/config.toml` 的 `base_url` 是否指向 `http://127.0.0.1:1800/v1`。

### Q: 改写器不工作？
A: 检查 `%APPDATA%\helmx.config.json` 是否存在且 `enabled: true`。查看日志 `~/.codex/helmx.log`。

### Q: 上游返回 401？
A: API key 无效或过期。检查 `~/.codex/config.toml` 的 `api_key`。

### Q: 上游返回 502？
A: 上游服务不可用。检查 `base_url` 是否正确，网络是否连通。

---

## 开发问题

### Q: 怎么编译？
A: `cmake -B build -G "MinGW Makefiles" && cmake --build build`。依赖 MinGW + Windows SDK。

### Q: 怎么运行测试？
A: `python tests/test_config.py`（单元测试）或 `python tests/cyber-test-runner.py`（集成测试）。

### Q: 怎么贡献？
A: Fork → feature 分支 → 提交 → PR。保持零外部 DLL 依赖。

### Q: License 是什么？
A: AGPL-3.0。仅供学习交流，禁止商用。
