import { describe, expect, it, vi } from 'vitest'
import { useRequestStream } from './useRequestStream'

// EventSource 在 jsdom 里不存在，这里用一个可控替身：
// 只验证三条契约 —— URL 拼对、坏帧不炸、断线后自己停（不重试轰炸）。

class FakeES {
  static last: FakeES | null = null
  url: string
  onmessage: ((ev: { data: string }) => void) | null = null
  onerror: ((ev: unknown) => void) | null = null
  closed = false
  constructor(url: string) {
    this.url = url
    FakeES.last = this
  }
  close() {
    this.closed = true
  }
  emit(data: string) {
    this.onmessage?.({ data })
  }
  fail() {
    this.onerror?.({})
  }
}

describe('useRequestStream', () => {
  it('连到代理的 /__helmx/events，并按 id 派发记录', () => {
    ;(globalThis as unknown as { EventSource: unknown }).EventSource = FakeES
    const got: string[] = []
    const { start } = useRequestStream((rec) => got.push(rec.id))
    start('127.0.0.1:1800')

    const es = FakeES.last!
    expect(es.url).toBe('http://127.0.0.1:1800/__helmx/events')

    es.emit(JSON.stringify({ id: 'req_1', path: '/v1/responses' }))
    expect(got).toEqual(['req_1'])

    // 坏帧必须被吞掉（实时流是旁路，不能因为它打断页面）
    es.emit('{not json')
    expect(got).toEqual(['req_1'])

    // 缺 id 的帧也忽略
    es.emit(JSON.stringify({ path: 'x' }))
    expect(got).toEqual(['req_1'])
  })

  it('出错后关掉自己，交给调用方的轮询兜底', () => {
    ;(globalThis as unknown as { EventSource: unknown }).EventSource = FakeES
    const { start, stop } = useRequestStream(() => {})
    start('127.0.0.1:1800')
    const es = FakeES.last!
    es.fail()
    expect(es.closed).toBe(true)
    stop() // 幂等
  })

  it('没有监听地址时不连', () => {
    ;(globalThis as unknown as { EventSource: unknown }).EventSource = vi.fn()
    FakeES.last = null
    const { start } = useRequestStream(() => {})
    start('')
    expect(FakeES.last).toBeNull()
  })
})
