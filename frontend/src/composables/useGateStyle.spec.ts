import { describe, it, expect } from 'vitest'
import { useGateStyle, type GateRisk } from './useGateStyle'
import { useVerdictStyle } from './useVerdictStyle'

describe('useGateStyle', () => {
  const { styleOf, all } = useGateStyle()

  it('覆盖三档风险', () => {
    expect(all()).toHaveLength(3)
    expect(all()).toEqual(['none', 'low', 'high'])
  })

  // web-ui 规则：状态不能只靠颜色 —— 形状必须能独立区分。
  it('三态形状互不相同', () => {
    const dots = all().map((r) => styleOf(r).dot)
    expect(new Set(dots).size).toBe(dots.length)
  })

  // 闸门徽章会与判定徽章同表出现，形状撞车会让用户分不清两者。
  it('与判定七态的形状不冲突', () => {
    const verdictDots = new Set(useVerdictStyle().all().map((v) => useVerdictStyle().styleOf(v).dot))
    for (const r of all()) {
      expect(verdictDots.has(styleOf(r).dot), `闸门形状 ${styleOf(r).dot} 与判定形状重复`).toBe(false)
    }
  })

  it('三态语义色互不相同', () => {
    const colors = all().map((r) => styleOf(r).color)
    expect(new Set(colors).size).toBe(colors.length)
  })

  it('颜色只用语义 class，不含裸色值', () => {
    for (const r of all()) {
      expect(styleOf(r).color).toMatch(/^text-[a-z-]+(\/[0-9]+)?$/)
    }
  })

  it('每态都有中文界面词与说明', () => {
    for (const r of all()) {
      const s = styleOf(r)
      expect(s.label.length).toBeGreaterThan(0)
      expect(s.labelEn.length).toBeGreaterThan(0)
      expect(s.desc.length).toBeGreaterThan(0)
    }
  })

  it('未知输入退化为 none，不抛错', () => {
    expect(styleOf(undefined as unknown as GateRisk).label).toBe('无风险')
    expect(styleOf('weird' as GateRisk).label).toBe('无风险')
  })
})
