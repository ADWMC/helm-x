import { describe, it, expect, vi, afterEach } from 'vitest'

/**
 * 后端探测的回归测试。
 *
 * 这个模块曾经连续错三次，每次都导致界面在某种环境下完全不可用：
 *
 *   v1 检查 window.__wails__        → 桌面窗口里也为 false，数据全变占位值
 *   v2 检查 window._wails 是否存在  → @wailsio/runtime 被打进 bundle，
 *                                    普通浏览器里也存在，恒为 true
 *   v2' 检查 content-type 是 JSON   → 静态服务器返回的结构化 404 也是 JSON，
 *                                    静态托管下被误判为桌面，卡在"读取中"
 *
 * 因此这里把判据本身锁死：只有 `/wails/runtime.js` 返回
 * **200 且 content-type 含 javascript** 才算桌面。这是 Wails 在
 * `internal/assetserver/bundled_assetserver.go:21-28` 里的实际行为。
 */

/** 每次以全新的模块实例测试（探测结果缓存在模块级）。 */
async function freshModule() {
  vi.resetModules()
  return await import('./backend')
}

afterEach(() => {
  vi.unstubAllGlobals()
})

/** 伪造响应：只需要被测代码会读的那几个字段。 */
interface StubResponse {
  ok?: boolean
  status?: number
  headers?: Record<string, string>
}

/**
 * 伪造 fetch。
 *
 * `headers` 收对象形式便于书写，内部转成 Headers 构造器要求的 [key, value][]。
 */
function stubFetch(impl: (url: string, init?: RequestInit) => StubResponse) {
  const calls: string[] = []
  vi.stubGlobal('fetch', (url: string, init?: RequestInit) => {
    calls.push(url)
    const r = impl(url, init)
    const entries = Object.entries(r.headers ?? {})
    return Promise.resolve({
      ok: r.ok ?? false,
      status: r.status ?? (r.ok ? 200 : 404),
      headers: new Headers(entries),
      text: () => Promise.resolve(''),
    } as Response)
  })
  return calls
}

describe('detectBackend', () => {
  it('探测 Wails 特有的 runtime.js 路径', async () => {
    const calls = stubFetch(() => ({ ok: true, headers: { 'content-type': 'application/javascript' } }))
    const m = await freshModule()
    await m.detectBackend()
    // 只断言本模块发起的探测。@wailsio/runtime 导入时会自行轮询
    // /wails/custom.js，那不是被测对象。
    expect(calls).toContain('/wails/runtime.js')
  })

  it('桌面环境：200 + javascript → 非演示模式', async () => {
    stubFetch(() => ({ ok: true, headers: { 'content-type': 'application/javascript' } }))
    const m = await freshModule()
    expect(await m.detectBackend()).toBe(true)
    expect(m.isDemo.value).toBe(false)
  })

  // 静态服务器对未知路径可能回 200（SPA 回落），但给的是 HTML。
  // 曾经因为只看 content-type 是否 JSON 而在这里判断失误。
  it('静态服务器 SPA 回落：200 但 content-type 是 html → 演示模式', async () => {
    stubFetch(() => ({ ok: true, headers: { 'content-type': 'text/html; charset=utf-8' } }))
    const m = await freshModule()
    expect(await m.detectBackend()).toBe(false)
    expect(m.isDemo.value).toBe(true)
  })

  it('静态服务器结构化 404（也是 JSON）→ 演示模式', async () => {
    stubFetch(() => ({ ok: false, status: 404, headers: { 'content-type': 'application/json' } }))
    const m = await freshModule()
    expect(await m.detectBackend()).toBe(false)
    expect(m.isDemo.value).toBe(true)
  })

  it('网络失败 → 演示模式，且不抛异常', async () => {
    vi.stubGlobal('fetch', () => Promise.reject(new Error('offline')))
    const m = await freshModule()
    expect(await m.detectBackend()).toBe(false)
    expect(m.isDemo.value).toBe(true)
  })

  it('结果被缓存，只探测一次', async () => {
    const calls = stubFetch(() => ({ ok: true, headers: { 'content-type': 'application/javascript' } }))
    const m = await freshModule()
    await m.detectBackend()
    await m.detectBackend()
    await m.detectBackend()
    expect(calls).toHaveLength(1)
  })

  // 首屏默认必须是演示模式：宁可不小心显示"预览模式"横幅再撤掉，
  // 也不要先渲染空数据再跳变。
  it('首屏默认按演示模式渲染', async () => {
    const m = await freshModule()
    expect(m.isDemo.value).toBe(true)
  })
})
