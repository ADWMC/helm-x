import { beforeEach, describe, expect, it } from 'vitest'
import { initTheme, setTheme, useTheme } from './useTheme'

// 主题的契约只有两条：**切了立刻生效**（data-theme 落到 <html>）、**偏好被记住**。
// 数值达标由 tools/contrast.py 断言（两套 token 都实测），这里不重复算对比度。

beforeEach(() => {
  localStorage.clear()
  document.documentElement.dataset.theme = ''
})

describe('useTheme', () => {
  it('initTheme 落默认浅色', () => {
    initTheme()
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('切深色 → data-theme=dark 且写入 localStorage', () => {
    setTheme('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem('helmx.theme')).toBe('dark')
    expect(useTheme().pref.value).toBe('dark')
  })

  it('切回浅色 → 覆盖旧偏好', () => {
    setTheme('dark')
    setTheme('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(localStorage.getItem('helmx.theme')).toBe('light')
  })

  it('system 档在无 matchMedia 的环境里退化为浅色而不是崩溃', () => {
    // jsdom 默认没有 matchMedia：useTheme 内部 try/catch，应回落到浅色
    setTheme('system')
    expect(['light', 'dark']).toContain(document.documentElement.dataset.theme ?? '')
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
