/**
 * 后端调用层：全前端唯一与 Go 通信的地方。
 *
 * 职责：
 *  1. 包装 Wails 生成的绑定，给页面一个稳定的接口
 *  2. 探测运行环境：桌面窗口（有绑定）还是浏览器预览（无绑定）
 *
 * 为什么需要第 2 点：`npm run dev` 直接开浏览器调试时没有 Wails 运行时，
 * 绑定调用会抛错。此时退化为演示数据，让界面仍可开发与走查。
 * **不退化为"假装成功"** —— 所有演示数据都带 demo 标记，界面上会提示。
 */

// 生成的绑定在两种模式下形态不同：
//   - `wails3 dev`        → .js（无类型）
//   - `wails3 build/package` → .ts（有类型）
//
// 直接 import 会在 dev 下报"找不到类型声明"。这里用一次显式转换收口，
// 换来本文件其余部分仍然类型安全（而不是整文件 any）。
//
// 注意：不能用 @ts-expect-error —— 生产模式下类型是存在的，
// 该指令会因"未使用"而报错（实测被 package 拦下）。
import * as BackendNS from '../../bindings/github.com/ADWMC/helm-x/internal/svc'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const Backend = BackendNS as any

import { ref } from 'vue'

type AnyRecord = Record<string, unknown>

/**
 * 是否运行在 Wails 桌面窗口中。
 *
 * 【两次都猜错了，记录在此避免再犯】
 *
 * v1：检查 `window.__wails__`。Wails v3 不注入这个全局，桌面里也返回 false，
 *     导致界面误报"浏览器预览模式"、数据全变占位值。
 *
 * v2：改为检查 `window._wails`。这个对象**确实存在** —— 但原因是
 *     `@wailsio/runtime` 被 Vite 打包进了主 bundle（实测 bundle 里
 *     `_wails` 出现 36 次），所以**普通浏览器里也存在**。
 *     于是 `isDesktop` 恒为 true，演示模式永远不激活，
 *     静态托管时界面卡在"读取中"并弹出 JSON 解析错误。
 *
 * 【现在的判据】不能看"有没有运行时对象"，也不能只看 content-type，
 * 要看**后端是否真的应答 Wails 特有的那个路径**。
 *
 * 判据的演变记录见下方 detectBackend 的注释。
 */
const WAILS_PROBE = '/wails/runtime.js'

let desktopCache: boolean | null = null

/**
 * 首屏默认值。
 *
 * 默认按**演示模式**渲染：宁可先显示"预览模式"横幅再撤掉，
 * 也不要先显示空数据再跳变。桌面窗口下探测通常在 1 帧内完成，
 * 用户看不到这次切换。
 */
const demoInitial = true

/**
 * 探测后端是否可用。首次调用发一次请求，之后走缓存。
 *
 * 【判据的演变，三次才对】
 *
 * v1 `window.__wails__`     → Wails v3 不注入该全局，桌面里也返回 false。
 * v2 `window._wails` 存在   → `@wailsio/runtime` 被 Vite 打进主 bundle，
 *                              普通浏览器里同样存在，恒为 true。
 * v2' content-type 是 JSON  → 本仓库的 tools/uiserve 对 /wails/* 返回的
 *                              也是 JSON（结构化 404），静态托管下误判为桌面。
 * v3 **GET /wails/runtime.js 返回 200 且 content-type 含 javascript**
 *
 * v3 直接对应 Wails 的实现
 * （`internal/assetserver/bundled_assetserver.go:21-28`：只有
 * `/wails/runtime.js` 这个精确路径返回 200 + application/javascript，
 * 其余 /wails/* 一律空响应）。静态服务器无法伪造这个组合。
 *
 * 在 main.ts 里于挂载前 await，九个页面因此不必各自处理时序。
 */
export async function detectBackend(): Promise<boolean> {
  if (desktopCache !== null) return desktopCache
  if (typeof window === 'undefined' || typeof fetch !== 'function') {
    desktopCache = false
    isDemo.value = true
    return false
  }
  try {
    const res = await fetch(WAILS_PROBE, { method: 'GET' })
    const ct = res.headers.get('content-type') || ''
    // 两个条件都要满足：静态服务器可能对未知路径回 200（SPA 回落），
    // 但 content-type 会是 text/html，不是 javascript。
    desktopCache = res.ok && ct.includes('javascript')
  } catch {
    desktopCache = false
  }
  isDemo.value = desktopCache !== true
  return desktopCache
}

/** 同步快照。首次探测完成前为 false。 */
export const isDesktop: boolean = false

/**
 * 当前是否使用演示数据。
 *
 * 用 `ref` 而非普通常量：探测是异步的，页面首次渲染时结果还没出来。
 * 用响应式变量，探测完成后横幅会自动出现，页面无需各自处理时序。
 *
 * 页面里的用法保持不变：`v-if="isDemo"`。
 */
export const isDemo = ref(demoInitial)

/** 同步判断（非响应式），供非组件代码使用。 */
export function isDemoNow(): boolean {
  return desktopCache !== true
}

function demoGuard<T>(fn: () => Promise<T>, fallback: T): Promise<T> {
  if (desktopCache !== true) return Promise.resolve(fallback)
  return fn()
}

// ── 类型（与 Go 侧 models.js 对应，此处显式声明便于页面使用）──

export type Verdict =
  | 'Healthy'
  | 'Refused'
  | 'Flagged'
  | 'UpstreamFailed'
  | 'Malformed'
  | 'Unresolved'

export interface ProxyStatus {
  running: boolean
  listen: string
  upstream: string
  requests: number
  byClass: Record<string, number>
  proxied: boolean
}

export interface CodexState {
  found: boolean
  home: string
  configPath: string
  provider: string
  baseUrl: string
  injected: boolean
  missingKeys: string[]
  hasBackup: boolean
  hasProxyBackup: boolean
  tables: number
  bytes: number
}

export interface WatchStatus {
  running: boolean
  intervalSec: number
  restores: number
  lastError?: string
}

export interface StatusView {
  proxy: ProxyStatus
  codex: CodexState
  watch: WatchStatus
}

export interface LogLine {
  ts: string
  level: string
  text: string
}

export interface RequestRecord {
  id: string
  ts: string
  path: string
  sessionId?: string
  inBytes: number
  outBytes: number
  injected: boolean
  class: Verdict
  action: string
  upstreamHits: number
  durationMs: number
  note?: string
}

export interface PromptModeView {
  id: string
  name: string
  description: string
  bytes: number
  default: boolean
  active: boolean
  preview: string
}

export interface RewriterView {
  enabled: boolean
  provider: string
  baseUrl: string
  model: string
  keyHint: string
  hasKey: boolean
  timeoutSec: number
  useProxy: boolean
  proxyUrl: string
  fallback: string
  maxAttempts: number
}

export interface RewriterInput {
  enabled: boolean
  provider: string
  baseUrl: string
  model: string
  apiKey: string
  timeoutSec: number
  useProxy: boolean
  proxyUrl: string
  fallback: string
  maxAttempts: number
}

export interface TestResult {
  ok: boolean
  input: string
  output: string
  error?: string
  durationMs: number
}

export interface VerifyProgress {
  step: number
  total: number
  name: string
  ok: boolean
  detail: string
  skip: boolean
  done?: boolean
  report?: { checks: unknown[]; failed: number }
}

export interface ConfigDiff {
  targetFile: string
  key: string
  oldValue: string
  newValue: string
  backupPath: string
  undoHint: string
}

export interface ApplyResult {
  ok: boolean
  changed: string[]
  message: string
}

export interface QAView {
  version: number
  updatedAt: string
  source: string
  items: { question: string; answer: string }[]
}

// ── 演示数据（仅浏览器预览时使用）──

const demoStatus: StatusView = {
  proxy: {
    running: false,
    listen: '127.0.0.1:1800',
    upstream: '(演示模式)',
    requests: 0,
    byClass: {},
    proxied: false,
  },
  codex: {
    found: false,
    home: '',
    configPath: '',
    provider: '',
    baseUrl: '',
    injected: false,
    missingKeys: [],
    hasBackup: false,
    hasProxyBackup: false,
    tables: 0,
    bytes: 0,
  },
  watch: { running: false, intervalSec: 60, restores: 0 },
}

// ── 对外接口 ──

export const api = {
  // 状态
  status: (): Promise<StatusView> => demoGuard(() => Backend.ProxyService.Status() as never, demoStatus),

  startProxy: () => demoGuard(() => Backend.ProxyService.Start() as never, undefined as never),
  stopProxy: () => demoGuard(() => Backend.ProxyService.Stop() as never, undefined as never),
  restartProxy: () => demoGuard(() => Backend.ProxyService.Restart() as never, undefined as never),
  setPassthrough: (on: boolean) =>
    demoGuard(() => Backend.ProxyService.SetPassthrough(on) as never, undefined as never),

  watchStart: (intervalSec: number) =>
    demoGuard(() => Backend.ProxyService.WatchStart(intervalSec) as never, undefined as never),
  watchStop: () => demoGuard(() => Backend.ProxyService.WatchStop() as never, undefined as never),

  // 配置
  configState: (): Promise<CodexState> => demoGuard(() => Backend.ConfigService.State() as never, demoStatus.codex),
  configPreview: (): Promise<ConfigDiff[]> =>
    demoGuard(() => Backend.ConfigService.Preview() as never, [] as never),
  configApply: (): Promise<ApplyResult> =>
    demoGuard(() => Backend.ConfigService.Apply() as never, { ok: false, changed: [], message: '演示模式' } as never),
  configRemove: (): Promise<ApplyResult> =>
    demoGuard(() => Backend.ConfigService.Remove() as never, { ok: false, changed: [], message: '演示模式' } as never),
  configRestoreProxy: (): Promise<ApplyResult> =>
    demoGuard(() => Backend.ConfigService.RestoreProxy() as never, { ok: false, changed: [], message: '演示模式' } as never),

  // 提示词
  promptModes: (): Promise<PromptModeView[]> =>
    demoGuard(() => Backend.PromptService.Modes() as never, [] as never),
  promptSet: (mode: string) => demoGuard(() => Backend.PromptService.Set(mode) as never, undefined as never),

  // 改写器
  rewriterGet: (): Promise<RewriterView> =>
    demoGuard(() => Backend.RewriterService.Get() as never, null as never),
  rewriterSet: (v: RewriterInput) => demoGuard(() => Backend.RewriterService.Set(v) as never, undefined as never),
  rewriterTest: (msg: string): Promise<TestResult> =>
    demoGuard(() => Backend.RewriterService.Test(msg) as never, { ok: false, input: msg, output: '', error: '演示模式', durationMs: 0 } as never),

  // 自检
  verifyRun: (e2e: boolean) => demoGuard(() => Backend.VerifyService.Run(e2e) as never, undefined as never),
  verifyReport: (): Promise<string> => demoGuard(() => Backend.VerifyService.Report() as never, '' as never),

  // 日志与请求
  logTail: (n: number): Promise<LogLine[]> => demoGuard(() => Backend.LogService.Tail(n) as never, [] as never),
  logPath: (): Promise<string> => demoGuard(() => Backend.LogService.Path() as never, '' as never),
  requests: (limit: number): Promise<RequestRecord[]> =>
    demoGuard(() => Backend.LogService.RecentRequests(limit) as never, [] as never),
  clearRequests: () => demoGuard(() => Backend.LogService.ClearRequests() as never, undefined as never),

  // QA
  qa: (): Promise<QAView> =>
    demoGuard(() => Backend.QAService.List() as never, { version: 0, updatedAt: '', source: '演示', items: [] } as never),
}

/** 事件名，与 Go 侧 svc 常量一致。 */
export const events = {
  log: 'proxy:log',
  request: 'proxy:request',
  status: 'proxy:status',
  verify: 'proxy:verify',
  restore: 'proxy:restore',
  configChanged: 'config:changed',
} as const


