# 文档索引

> helm-x-wails 重写的完整方案。**先读本页，再按需深入。**

---

## 一、方案文档

| 文档 | 回答什么 | 什么时候读 |
|---|---|---|
| [`PLAN.md`](PLAN.md) | **为什么这样重写**：旧版问题、不变量、架构、机制设计 | 想理解设计动机时 |
| [`PLAN-SPEC.md`](PLAN-SPEC.md) | **具体实现成什么**：数据模型、接口签名、事件契约、CLI、文件格式 | 写代码前 |
| [`PHASES.md`](PHASES.md) | **先做什么**：阶段任务清单、验收命令、阻塞项 | 每次开工前 |
| [`RISKS.md`](RISKS.md) | **什么会出错**：风险登记册、未验证清单 | 决策前 |
| [`ROADMAP.md`](ROADMAP.md) | **做到什么算完**：里程碑、范围边界、迁移路径 | 对齐范围时 |

## 二、界面文档

| 文档 | 回答什么 |
|---|---|
| [`DESIGN.md`](DESIGN.md) | **为什么这样设计 UI**：视觉层级、组件边界、交互规则、文案规则 |
| [`DESIGN-SPEC.md`](DESIGN-SPEC.md) | **UI 具体写成什么**：token 表、组件签名、页面清单、验收 |

## 三、待产出

| 文档 | 内容 | 状态 |
|---|---|---|
| [`FINDINGS.md`](FINDINGS.md) | 阶段 A：P1–P4 的实测结论 | ✅ 已完成 |
| [`FINDINGS-phase1.md`](FINDINGS-phase1.md) | 真实 codex 请求体形态与注入有效性 | ✅ 已完成 |
| [`adr/ADR-001-toml-handling.md`](adr/ADR-001-toml-handling.md) | 为什么自建行级 TOML 模型 | ✅ 已完成 |

---

## 四、当前状态

| 项 | 状态 |
|---|---|
| 旧版代码 | 已通读（~3100 行手写核心 + 841 行 dashboard） |
| 重写方案 | 已完成 |
| 前端骨架 | **已完成并接线**：11 单元测试 + 类型检查通过 |
| **Go 后端** | **已完成**：协议层 / 代理管线 / TAMPER / 改写器 / 配置 / 自检 / 守护 / CLI |
| **桌面界面** | **已完成**：Wails 窗口 + 10 页面 + 托盘，已实测运行 |
| P1–P4 验证 | **已完成**：P1/P4 证伪，P2/P3 确认（见 `FINDINGS.md`） |

### 已验证的事实（跑过命令，非声称）

| 项 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` | exit 0 |
| `gofmt -l` | 无输出 |
| Go 单元测试 | jsonwalk / sse / codexcfg / proxy / tamper 全绿 |
| 前端 `vitest` | 11 passed |
| 前端 `vue-tsc --noEmit` | exit 0 |
| **真 codex → 本代理 → 真上游** | **成功**（回复 `E2E-OK`，日志 `class=Healthy`） |
| **`apply` → `remove` 逐字节还原** | **成功**（SHA256 与原文件一致，基于真实 173 行 / 58 表配置） |
| **崩溃恢复** | **成功**（强杀后 `proxy --restore` 仍逐字节还原） |
| **桌面窗口** | **成功**（WailsWebviewWindow，加载真实配置数据） |
| **`wails3 task package`** | **成功**：产出 `bin/helm-x-amd64-installer.exe`（6.71 MB，NSIS 安装程序） |
| **生产版 exe** | **成功**：`bin/helmx.exe` 12.65 MB，版本资源正确嵌入 |

### 未验证的假设（详见 `RISKS.md` §5）

- 长会话下首段缓冲窗口的表现（2048B / 400ms 是否合用）
- 安装程序在干净机器上的实际安装流程（未在无 WebView2 的环境试装）
- 60 题基线能否复现（需要真实上游与时间）
- 改写器端到端效果（需要 LLM API key）

---

## 五、构建与运行

```powershell
# 1. 构建前端
cd frontend
npm install
npm run build        # 产出 frontend/dist，被 go:embed 打进二进制

# 2. 构建二进制
cd ..
go build -o helmx.exe .

# 3. 运行
.\helmx.exe              # 桌面窗口
.\helmx.exe proxy        # 无窗口代理
.\helmx.exe verify       # 自检
```

**注意**：`main.go` 会 `go:embed all:frontend/dist`。
若前端未构建，嵌入目录缺 `index.html`，此时窗口显示兜底提示页，
**命令行功能不受影响**。

### 用 wails3 构建与打包

```powershell
wails3 task build       # → bin/helmx.exe
wails3 task package     # → bin/helm-x-amd64-installer.exe（NSIS 安装程序）
wails3 task dev         # 开发模式，前端热重载
wails3 task test        # Go + 前端全部测试
wails3 task verify      # go vet + 前端类型检查
```

**打包前置条件**：

| 依赖 | 用途 | 安装 |
|---|---|---|
| NSIS（`makensis`） | 生成安装程序，**必须在 PATH 中** | `winget install NSIS.NSIS` |
| WebView2 Bootstrapper | 随安装包分发，由 wails3 自动下载 | 自动 |

NSIS 装完后**新开的终端**才有 PATH：

```powershell
winget install NSIS.NSIS          # 装到 C:\Program Files (x86)\NSIS
# 若 winget 未自动写入 PATH，手动加到用户级：
[Environment]::SetEnvironmentVariable('Path',
  "$([Environment]::GetEnvironmentVariable('Path','User'));C:\Program Files (x86)\NSIS", 'User')
```

验证：`makensis /VERSION` 应输出 `v3.12`。

### build/ 目录的隔离机制（改动前必读）

`build/` 由 `wails3 generate build-assets` 生成。其中 `build/ios` 与 `build/android`
含 Wails 的平台模板 Go 文件，会导致 `go build ./...` 失败：

```
build/ios/app_options_default.go 有 //go:build !ios
→ 在 Windows 上参与编译
→ 该目录是 package main 却没有 main 函数
→ function main is undeclared in the main package
```

**不要删除这些目录** —— 图标生成无条件写 `darwin/icons.icns`，删了打包会挂。

当前解法：给 `build/ios` 和 `build/android` 各放一个独立 `go.mod`，
利用 Go 的 module 边界规则把它们排除在 `./...` 之外。

⚠️ **重新运行 `wails3 generate build-assets` 会覆盖这两个 `go.mod`**，
之后必须重新添加，否则 `go build ./...` 会再次失败。

`build/` 内其余文件（平台 Taskfile、图标、清单、NSIS 模板）
**不要手工编辑**，改元数据请重新生成：

```powershell
wails3 generate build-assets -name helm-x -binaryname helmx `
  -productname "helm-x" -productdescription "codex 本地映射层" ...
```

重新生成绑定（改动 Go 侧服务方法签名后必须执行）：

```powershell
wails3 generate bindings
```

**绑定形态随模式变化**：`dev` 产出 `.js`（无类型），`build/package` 产出 `.ts`（有类型）。
`frontend/src/api/wails-bindings.d.ts` 为前者提供兜底声明；
后者有真实类型时该声明自动让位。**改动此处前先在两种模式下都跑一次 `vue-tsc`。**

---

## 六、阅读顺序建议

**初次接触**：`ROADMAP.md` → `PLAN.md` §0–2 → `DESIGN.md` §0-A

**准备写 Go 代码**：`PLAN.md` §5 → `PLAN-SPEC.md` → `PHASES.md` 对应阶段

**准备改界面**：`DESIGN.md` → `DESIGN-SPEC.md`

**做决策**：`RISKS.md` → `ROADMAP.md` §3.3

---

## 七、硬规则速查

改动前对照：

1. **不变量优先**：`PLAN.md` §1 的 INV-1…INV-10，改动若违反其一即为错
2. **先证后改**：判"旧版是 X"必须给行号或截图；推断要标注为推断
3. **看渲染结果**：判 UI 好坏必须截图（读 CSS 读不出层级问题）
4. **未跑命令不算完成**：`PHASES.md` 全局 DoD
5. **文档与实现不一致时改文档**：不留与事实不符的方案


