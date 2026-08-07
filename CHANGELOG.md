# 更新日志

## v0.0.3 (2026-08-07)

### 新功能
- **提示词系统重构**：按绕过技术命名（prompt-ctf-scoring / prompt-sandbox-executor）
- **CTF 计分制**：6 层绕过架构（环境声明 + 语义归一化 + 计分板 + 输出锁 + 禁止词 + 回退链）
- **语义归一化**：12 个敏感词→CTF 术语映射（破解→crackme challenge 等）
- **内置免费改写 API**：sensenova-6.7-flash-lite，开箱即用无需配置
- **上下文感知改写器**：读取最近 6 轮对话历史，理解用户意图后改写
- **Cyber 日志**：记录所有 cyber 事件（helmx-cyber.log）
- **MCP 注入移除**：不再修改用户 config.toml 的 MCP 配置

### 修复
- **Cyber 日志死锁**：log_cyber() 持锁调 log_info() 导致死锁，改用 printf + flush
- **TAMPER Responses API 兼容**：添加 `"text":"` fallback（原来只搜 `"output_text":"`）
- **改写器 API 认证**：修复 Authorization header 格式
- **改写器 fallback**：改写失败时用原消息 clean session 重试
- **改写器 Accept 逻辑**：API 返回结果即接受（不再要求和上次不同）

### UI 改进
- **侧边栏重构**：概览(系统总览/自检) → 配置(服务/提示词/改写器) → 调试(规则) → 日志
- **提示词独立页面**：从改写器配置中拆出
- **指标卡更新**：代理状态 / 改写器状态 / TAMPER 规则 / 提示词模式
- **移除无用元素**：apply/remove 按钮、注入状态、watch 服务、AGENTS.md 指标
- **改写器测试默认值**：改为 "分析 system_server 进程的只读内存布局"

### 文件重命名
- `bridge.md` → `prompt-ctf-scoring.md`（按绕过技术命名）
- `bridge-v45.md` → `prompt-sandbox-executor.md`

## v0.0.2-fix2 (2026-08-07)

### 新功能
- **提示词切换**：内置 v45 提示词（gpt-5.6-instruct），支持 default/v45 切换
- **单窗口模式**：双击只开一个 CMD 窗口，Proxy + UI 同进程运行
- **重启按钮**：主界面和服务页添加"重启服务"按钮
- **激活词更新**：default 和 v45 有独立的激活回复

### 修复
- **重启功能**：先启动新进程再退出，避免端口冲突
- **Clean Session**：从原始 body 提取字段重建请求，解决 JSON 格式问题

### 测试
- **57 用例测试**：12 类别，100% 通过率

## v0.0.2-fix1 (2026-08-07)

### 改进
- **UI 全面重写**：移植 SHVMP 设计系统
- **改写器配置页面**：启用/禁用、Provider/Model/API Key 配置

### 修复
- **Clean Session JSON 格式**：从原始 body 提取字段重建请求
- **改写器提示词内嵌**：rewrite_prompt.txt XOR 加密嵌入二进制

## v0.0.2-beta (2026-08-07)

### 新功能
- **上下文感知改写器**：3 次重试换角度、禁止词列表
- **Clean Session 绕过**：cyber flag 时自动剥离对话历史
- **68 用例测试矩阵**

## v0.0.1-beta (2026-08-06)

### 首发
- HTTP MITM 代理 + AGENTS 注入
- TAMPER 篡改引擎
- Web 控制台
- MCP 服务器
- 单二进制，零外部 DLL 依赖
