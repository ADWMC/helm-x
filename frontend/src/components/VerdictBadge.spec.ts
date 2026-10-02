import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import VerdictBadge from './VerdictBadge.vue'
import { useVerdictStyle } from '@/composables/useVerdictStyle'

// 注意：wrapper.attributes() 取的是多根组件的外层包装，不是渲染出的根元素。
// 断言属性/文本一律走 wrapper.find('span')。
describe('VerdictBadge', () => {
  const { all, styleOf } = useVerdictStyle()

  it('渲染判定对应的圆点与界面词', () => {
    const w = mount(VerdictBadge, { props: { verdict: 'Refused' } })
    expect(w.find('span').text()).toContain('◍')
    expect(w.find('span').text()).toContain('已改写')
  })

  it('六种状态都能渲染出各自的形状', () => {
    for (const v of all()) {
      const w = mount(VerdictBadge, { props: { verdict: v } })
      expect(w.find('span').text()).toContain(styleOf(v).dot)
      expect(w.find('span').text()).toContain(styleOf(v).label)
    }
  })

  // dotOnly 用于密集表格：视觉上只留圆点。
  // 无障碍名字由 aria-label 承载，而不是塞 .sr-only 子元素
  // —— 子元素文本会被祖先 textContent 收集，等于没隐藏。
  it('dotOnly 不渲染可见文字，改用 aria-label 承载状态', () => {
    const w = mount(VerdictBadge, { props: { verdict: 'Flagged', dotOnly: true } })
    expect(w.find('span').text()).toBe('◉')
    expect(w.find('span').attributes('aria-label')).toBe('已重建会话')
  })

  it('非 dotOnly 时不加多余 aria-label（文字已可读）', () => {
    const w = mount(VerdictBadge, { props: { verdict: 'Healthy' } })
    expect(w.find('span').text()).toContain('正常')
    expect(w.find('span').attributes('aria-label')).toBeUndefined()
  })

  it('带上说明文案作为 tooltip', () => {
    const w = mount(VerdictBadge, { props: { verdict: 'Unresolved' } })
    expect(w.find('span').attributes('title')).toBe(styleOf('Unresolved').desc)
  })
})
