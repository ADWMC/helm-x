# 更新日志

## v0.0.2-fix2 (2026-08-07)

### 新功能
- **提示词切换**：内置 v45 提示词（gpt-5.6-instruct），支持 default/v45 切换
- **单窗口模式**：双击只开一个 CMD 窗口，Proxy + UI 同进程运行
- **重启按钮**：主界面和服务页添加"重启服务"按钮
- **主界面提示词显示**：指标条显示当前提示词模式
- **激活词更新**：default 和 v45 有独立的激活回复

### 修复
- **重启功能**：先启动新进程再退出，避免端口冲突
- **激活验证**：检测新激活词（helm-x online / v45 online）
- **Clean Session**：从原始 body 提取字段重建请求，解决 JSON 格式问题

### 测试
- **57 用例测试**：12 类别，100% 通过率
- **v45 提示词**：CS2 ESP 请求通过（default 模式拒绝）

## v0.0.2-fix1 (2026-08-07)

### 改进
- **UI 全面重写**：移植 SHVMP 设计系统（浅色/暗色主题、响应式、动画、metric-strip）
- **改写器配置页面**：启用/禁用、Provider/Model/API Key/Base URL/Proxy URL/Timeout 配置
- **系统字体**：全部使用系统字体栈（ui-monospace、-apple-system、PingFang SC）
- **改写器模型要求文档**：添加推荐模型和注意事项

### 修复
- **Clean Session JSON 格式**：从原始 body 提取字段重建请求，避免 JSON 解析错误
- **改写器提示词内嵌**：rewrite_prompt.txt XOR 加密嵌入二进制
- **版本号统一**：version.h 单一来源（CMake/MCP/User-Agent）

### 测试
- **改写器成功率**：91.4% (32/35)
- **总通过率**：100% (10/10)
- **AGENTS 注入率**：100% (37/37)

### 文档
- **README**：添加改写器模型要求、推荐模型、配置说明
- **FAQ**：20+ 常见问题
- **ARCHITECTURE**：架构图、请求流程、数据流
- **TEST_STANDARD**：测试标准、评分算法

## v0.0.2-beta (2026-08-07)

### 新功能
- **上下文感知改写器**：参考 gptbypass，支持拒绝文本传入、3 次重试换角度、禁止词列表
- **Clean Session 绕过**：cyber flag 触发时自动剥离对话历史、保留上游字段重建请求
- **改写提示词内嵌**：rewrite_prompt.txt XOR 加密嵌入二进制，无需外部文件
- **68 用例测试矩阵**：cyber-test-matrix-v2.md，覆盖 7 个类别
- **版本号统一管理**：version.h 单一来源（CMake/MCP/User-Agent 全部引用）

### 改进
- AGENTS 注入直接阻止大部分 cyber flag（8/8 高危请求通过）
- 改写器加载优先级：config > 内嵌资源 > 外部文件
- 代理端口可配置：helmx.config.json 的 `proxy_url` 字段（默认 127.0.0.1:7897）
- README 更新测试结果和架构说明
- README_EN 同步英文版

### 修复
- 修复 cyber 重试时重新注入 AGENTS 导致二次触发
- 修复 clean session 丢失上游字段（tools/reasoning）
- 修复 rewrite.cpp 硬编码代理端口 7897

### 致谢
- [gptbypass](https://github.com/null119/gptbypass) — 改写器提示词与拒绝词检测
- [gpt-5.6-instruct](https://github.com/MDX-Tom/gpt-5.6-instruct) — 测试方法论

## v0.0.1-beta (2026-08-06)

### 首发
- HTTP MITM 代理 + AGENTS 注入
- TAMPER 篡改引擎（拒绝响应改写）
- Web 控制台（Clash Verge 风格）
- MCP 服务器（6 内置工具）
- 自愈守护进程
- XOR 加密内嵌资源
- 单二进制，零外部 DLL 依赖

### 致谢
- [NERV-BREAK-5.6](https://github.com/lingbol088-spec/5.6-JAILBREAK-NERV-codex-instruct-5.6)
- [Codex-X](https://github.com/yynxxxxx/Codex-X)
