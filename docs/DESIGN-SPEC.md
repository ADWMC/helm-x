# helm-x-wails 设计规格

> **本文将 `DESIGN.md` 的规则落成可实现的规格。**
> `DESIGN.md` 回答"为什么这样设计"，本文回答"具体写成什么"。
> 两者的关系：`DESIGN.md` 是契约，本文是契约的实现说明 + token 表 + 组件签名。

---

## 1. 技术栈与初始化

```jsonc
// frontend/package.json（关键依赖，锁次版本）
{
  "dependencies": {
    "vue": "^3.5.0",
    "vue-router": "^4.4.0",
    "pinia": "^2.2.0"
  },
  "devDependencies": {
    "vite": "^7.0.0",
    "@vitejs/plugin-vue": "^6.0.0",
    "typescript": "^5.6.0",
    "tailwindcss": "~4.3.3",
    "@tailwindcss/vite": "~4.3.3",
    "daisyui": "~5.7.47",
    "vitest": "^2.1.0",
    "@vue/test-utils": "^2.4.0"
  }
}
```

**Tailwind 4 不生成 `tailwind.config.js`** —— 配置全在 CSS（`@plugin`）。网上 v3 的 daisyUI 教程不能直接抄。

```ts
// frontend/vite.config.ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  base: './',           // Wails 用 file 协议加载资源，必须相对路径
  build: { outDir: 'dist', emptyOutDir: true },
  test: { environment: 'jsdom', globals: true },
})
```

---

## 2. 主题 token

```css
/* frontend/src/style.css */
@import "tailwindcss";

@plugin "daisyui" {
  themes: light --default, dark --prefersdark;
  /* 只打进用到的组件，控制 CSS 体积 */
  include: button, badge, card, input, select, textarea, toggle,
           table, tabs, alert, modal, tooltip, progress, loading,
           collapse, menu, navbar, drawer, stat, indicator, join,
           fieldset, label, divider, status, toast, list;
}

/* 品牌色：沿用原 dashboard 的 accent，不自造 */
@plugin "daisyui/theme" {
  name: "light";
  default: true;
  color-scheme: light;
  --color-primary: oklch(60% 0.118 165);        /* ≈ #10a37f */
  --color-primary-content: oklch(98% 0.01 165);
  --radius-box: 0.75rem;                         /* 原版 radius-md 16px → 收敛到 12px */
  --radius-field: 0.5rem;
}

@plugin "daisyui/theme" {
  name: "dark";
  prefersdark: true;
  color-scheme: dark;
  --color-primary: oklch(60% 0.118 165);
  --color-primary-content: oklch(98% 0.01 165);
  --radius-box: 0.75rem;
  --radius-field: 0.5rem;
}
```

```css
/* 全局：原版 dashboard.html:228 已有此规则，换库后要接上 */
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: .01ms !important;
    transition-duration: .01ms !important;
  }
}
```

---

## 3. 尺寸 token（取原版实测值，不重定）

| 用途 | 值 | 来源 |
|---|---|---|
| 侧边栏宽 | `236px` | `.app-shell` grid 第一列 |
| 侧边栏内边距 | `24px 14px 18px` | `.sidebar` |
| 导航项高 | `42px`（`min-height`） | `.nav-button` |
| 导航项间距 | `4px` | `.nav-list` gap |
| topbar 高 | `56px` | `.topbar` `min-height` |
| topbar 内边距 | `0 32px` | `.topbar` |
| 内容区内边距 | `32px 32px 48px` | `.content` |
| 面板间距 | `24px` | `.panel` `margin-bottom` |
| 面板头高 | `56px` | `.panel-head` |
| 面板内边距 | `18px` | `.panel-body` |
| 表格头内边距 | `10px 18px` | `thead th` |
| 表格行内边距 | `14px 18px` | `tbody td` |
| 日志字号 | `11.5px / 1.6` | `.term` |
| 日志最大高 | `320px` | `.term` |
| 圆点直径 | `6px` | `.status-dot` |

**断点**：`960px`（侧边栏转抽屉）、`680px`（隐藏 topbar 状态、单列）。

---

## 4. 判定状态映射（唯一实现）

```ts
// frontend/src/composables/useVerdictStyle.ts
export type Verdict =
  | 'Healthy' | 'Refused' | 'Flagged'
  | 'UpstreamFailed' | 'Malformed' | 'Unresolved'

interface VerdictStyle {
  dot: string          // 形状，保证色盲可读
  color: string        // 文本色 class
  label: string        // 中文界面词
  labelEn: string
  desc: string         // tooltip 用
}

const VERDICT: Record<Verdict, VerdictStyle> = {
  Healthy: {
    dot: '●', color: 'text-success', label: '正常', labelEn: 'Passed',
    desc: '上游响应正常，原样转发',
  },
  Refused: {
    dot: '●', color: 'text-warning', label: '已改写', labelEn: 'Rewritten',
    desc: '命中拒绝规则，TAMPER 已改写',
  },
  Flagged: {
    dot: '●', color: 'text-error', label: '已重建会话', labelEn: 'Resent',
    desc: '触发安全标记，已换会话重发',
  },
  UpstreamFailed: {
    dot: '○', color: 'text-error', label: '上游失败', labelEn: 'Upstream failed',
    desc: '未拿到有效响应，已重试',
  },
  Malformed: {
    dot: '◌', color: 'text-base-content/60', label: '无法解析', labelEn: 'Unparsed',
    desc: '响应形态不认识，原样透传',
  },
  Unresolved: {
    dot: '◐', color: 'text-warning', label: '未补救', labelEn: 'Unresolved',
    desc: '三级补救均失败，已原样返回模型输出',
  },
}

export function useVerdictStyle() {
  return {
    styleOf: (v: Verdict): VerdictStyle => VERDICT[v],
    all: () => Object.keys(VERDICT) as Verdict[],
  }
}
```

**约束**：`VerdictBadge` 是唯一消费者。其他组件不得内联颜色判断。

---

## 5. 组件规格

### 5.1 `VerdictBadge`

```
props:  { verdict: Verdict, showLabel?: boolean = true }
渲染:   <span class="inline-flex items-center gap-1.5 font-mono text-[10px]">
          <span :class="style.color">{{ style.dot }}</span>
          <span v-if="showLabel" :class="style.color">{{ style.label }}</span>
        </span>
禁止:   接受 color / icon / class 之类的样式覆盖 prop
```

### 5.2 `StatTile`

```
props:  { label: string, value: string, hint?: string, tone?: 'default'|'ok'|'bad' }
渲染:   标签 11px muted / 值 18px 600 / 提示 11px
约束:   值不得为空串。无数据时传 '未配置' 并把下一步动作放 hint（§7.1）
```

### 5.3 `LogStream`

```
props:  { lines: LogLine[], follow?: boolean = true, max?: number = 2000 }
暴露:   pause() / resume() / clear()
行为:   · 默认跟随，用户上滚自动暂停
        · 暂停时顶部显示「已暂停 · N 条新记录」，点击回到底部恢复
        · 超过 max 丢弃最旧，顶部标注「仅显示最近 2000 条」
        · 暂停期间新行进缓冲，不丢
渲染:   <pre class="font-mono text-[11.5px] leading-[1.6] max-h-[320px] overflow-y-auto">
```

### 5.4 `SectionCard`

```
props:  { title: string, meta?: string, action?: slot }
slots:  default（内容）、action（右上角操作）
渲染:   面板头 56px + border-b；内容 18px padding
约束:   不得嵌套 SectionCard（§1.3 禁止卡片套卡片）
```

### 5.5 `ConfigField`（本项目特有）

```
props:  { label: string, current: string, next?: string, targetFile: string }
渲染:   标签 + 当前值
        next 存在时显示「将写入：{next}」
        targetFile 始终以等宽小字显示（用户必须知道改的是哪个文件）
```

### 5.6 `DangerZone`

```
props:  { title: string }
slots:  default
渲染:   border-error/40 区块 + 标题 + 后果说明位
约束:   内部按钮一律 btn-error btn-outline（§1.2）
```

### 5.7 `AppShell`

```
结构:   <div class="grid grid-cols-1 md:grid-cols-[236px_minmax(0,1fr)] min-h-screen">
          <Sidebar />
          <div class="flex flex-col min-w-0">
            <Topbar />
            <main class="flex-1 p-8 pb-12"><RouterView /></main>
          </div>
        </div>
导航:   10 项，按组（概览 / 配置 / 调试 / 日志）
        每项：图标 + 文字 + 状态点（有异常才显示）
键盘:   1–9,0 切页；/ 聚焦搜索；Esc 关模态
```

**Topbar 规则**（修 V3）：只放移动端汉堡 + 面包屑（小号）+ 右侧全局状态。**不得与页面 h1 同字**。

---

## 6. 页面清单与主操作

| 页 | 路由 | 主操作 | 次级 |
|---|---|---|---|
| 总览 | `/` | 无（只读） | 重启服务 |
| 请求 | `/requests` | 无（只读） | 过滤 / 导出 |
| 验证 | `/verify` | 运行自检 | e2e 验证 |
| 服务 | `/services` | 启动/停止代理 | 重试参数、改写器开关 |
| 提示词 | `/prompts` | 应用模式 | 预览 |
| 上下文 | `/context` | 保存 | 各项阈值 |
| QA | `/qa` | 检查更新 | 搜索 |
| 改写器 | `/rewriter` | 保存并测试 | — |
| 日志 | `/logs` | 暂停/跟随 | 清空 |

---

## 7. 交付顺序

1. `style.css` — 主题 token（§2）+ 尺寸 token（§3）
2. `useVerdictStyle.ts` + Vitest 单测（六态齐全、形状各不相同）
3. `AppShell` + `Sidebar` + `Topbar`
4. 4 个基础组件：`VerdictBadge` / `StatTile` / `LogStream` / `SectionCard`
5. **截图确认骨架**，再铺 10 个页面

第 5 步是硬门：骨架没截图确认就铺页面，会长成 10 种样子。

---

## 8. 验收

| 项 | 方法 |
|---|---|
| 六态渲染正确 | Vitest 断言每个 `Verdict` 的 dot 唯一且 label 正确 |
| 深浅两模式 | 两种 `prefers-color-scheme` 各截一张，对比层级 |
| 首屏非空 | 无数据时总览页显示空状态文案，**不出现大卡片占位** |
| 无主动模态 | 遍历页面，除危险操作确认外不得弹模态 |
| 标题唯一 | 每页 topbar 文案 ≠ h1 文案 |
| 键盘可达 | 全部交互元素可 Tab；模态内焦点锁定 |
| 对比度 | 正文 ≥ 4.5:1；`badge` / 状态文字单独核对 |

截图命令见 `DESIGN.md` §10.2。

---

## 9. 骨架已验证的事实

阶段 1–4 已跑通（`frontend/`）。以下是**实测**结果，非计划：

| 项 | 命令 | 结果 |
|---|---|---|
| 依赖安装 | `npm install` | 187 包，无 error |
| 类型检查 | `npx vue-tsc --noEmit` | 干净 |
| 单元测试 | `npx vitest run` | **11 passed**（2 文件） |
| 生产构建 | `npx vite build` | 63 模块，`index.css 47.66 kB` gzip 8.24 kB，主 JS 102.88 kB gzip 40.40 kB |
| 深色渲染 | headless 截图 | 正常 |
| 浅色渲染 | headless 截图（`data-theme="light"`） | 正常 |

**过程中被测试拦下的两个真实缺陷**（不是假设）：

1. **六态里有三个共用 `●`** —— `Healthy/Refused/Flagged` 都是实心圆，等于退回"只靠颜色"，
   直接违反本规格 §4 的第 1 条硬约束。`useVerdictStyle.spec.ts` 的"形状互不相同"用例拦下。
   已改为 `● ◍ ◉ ○ ◌ ◐`，六种形状互异。
2. **`dotOnly` 用 `.sr-only` 子元素承载无障碍文本是无效的** —— 子元素文本仍会被祖先
   `textContent` 收集，并没有真的隐藏。已改为在外层用 `aria-label`。

### 9.1 三个环境/写法陷阱（已踩，记录以免重犯）

| 陷阱 | 症状 | 处理 |
|---|---|---|
| `vitest@2` 自带一份 `vite` | `vue-tsc` 报 `Plugin<Api>` 类型不兼容，报错长到不可读 | 升 `vitest@^5`（peer 接受 vite 7），重复副本消失 |
| `defineConfig` 从 `vite` 导入 | `test` 字段不被 `UserConfigExport` 接受 | 从 `vitest/config` 导入 |
| `wrapper.attributes()` 用于多根组件 | 取到的是外层包装，属性返回 `undefined`，让人误以为组件坏了 | 断言一律走 `wrapper.find('span')` |

**截图工具注意**：`--force-prefers-color-scheme=light` 在本机 headless 下**无效**（输出与深色逐字节相同，
MD5 一致）。验证浅色要显式给根元素加 `data-theme="light"`，或改用 CDP 的
`Emulation.setEmulatedMedia`。**不要因为截图"看起来对"就以为切主题生效了 —— 比 hash。**

