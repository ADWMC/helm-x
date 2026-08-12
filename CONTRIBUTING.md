# Contributing to helm-x

**欢迎 PR！** 本项目欢迎任何形式的贡献：Bug 修复、新功能、文档改进、TAMPER 规则扩充、测试补充。

## 你可以贡献的方向

- **TAMPER 规则**：扩充 `tamper.cpp` 的拒绝句式匹配（当前只有 1 条占位正则）
- **UI 改进**：`assets/dashboard.html` 的界面与交互
- **proxy 增强**：SSE 流式转发、多上游轮询、请求日志审计
- **文档**：README、使用示例、FAQ
- **测试**：verify 自检项扩充、端到端测试脚本

## 开发环境

- Windows 10/11
- MinGW g++ 16+（静态链接）
- CMake 3.16+
- Python 3（仅用于 `tools/embed.py` 资源加密生成）

```bat
:: 构建
python tools/embed.py
cmake -B build -G "MinGW Makefiles" -DCMAKE_BUILD_TYPE=Release
cmake --build build
```

## 流程

1. Fork 本仓库
2. 创建 feature 分支：`git checkout -b feat/xxx`
3. 提交修改（遵循现有代码风格：C++17、零外部依赖、保持单二进制）
4. 推送并创建 Pull Request
5. 在 PR 描述中说明改动内容 + 测试结果

## 约定

- 保持零外部 DLL 依赖（只用系统库 KERNEL32/UCRT/WS2_32/SHELL32/WINHTTP）
- 新增资源走 `tools/embed.py` 加密内嵌
- 提交信息用英文，描述清楚改动与验证
- 大改动先开 issue 讨论
- 修改 `assets/bridge.md` 前注意：这是运行时注入内容，需实测不破坏现有能力

## 许可证

本项目为 GNU AGPL v3.0。提交代码即视为同意在 AGPL v3.0 下授权。

> ⚠️ 仅供学习交流，禁止商用。
