import { describe, it, expect } from 'vitest'
import { useVerdictStyle, type Verdict } from './useVerdictStyle'

describe('useVerdictStyle', () => {
  const { styleOf, all } = useVerdictStyle()

  it('覆盖全部六种判定状态', () => {
    expect(all()).toHaveLength(6)
  })

  // web-ui 规则 6：状态不能只靠颜色。
  // 六种状态的形状必须互不相同，否则色盲用户读不出差异。
  it('每种的形状标记互不相同', () => {
    const dots = all().map((v) => styleOf(v).dot)
    expect(new Set(dots).size).toBe(dots.length)
  })

  // 每种状态必须有独立的中文界面词，避免两个状态显示成同一个词。
  it('每种的界面词互不相同', () => {
    const labels = all().map((v) => styleOf(v).label)
    expect(new Set(labels).size).toBe(labels.length)
  })

  // 颜色也必须能区分语义档位。
  // 原实现 Refused/Unresolved 同为 warning、Flagged/UpstreamFailed 同为 error，
  // 形状虽不同，但扫一眼颜色会把「处理成功了」和「没处理好」混为一谈。
  it('每种的语义色互不相同', () => {
    const colors = all().map((v) => styleOf(v).color)
    const dup = colors.filter((c, i) => colors.indexOf(c) !== i)
    expect(dup, `以下颜色被重复使用：${[...new Set(dup)].join(', ')}`).toHaveLength(0)
  })

  // 语义档位：这两档是用户最需要一眼分辨的，不能被挪到别的色。
  it('正常用 success，上游失败用 error', () => {
    expect(styleOf('Healthy').color).toBe('text-success')
    expect(styleOf('UpstreamFailed').color).toBe('text-error')
  })

  // 「已改写 / 已重建」是处理成功，不该用 error 或 warning 这类报警色。
  it('处理成功的状态不用报警色', () => {
    for (const v of ['Refused', 'Flagged'] as Verdict[]) {
      expect(styleOf(v).color).not.toMatch(/error|warning/)
    }
  })

  it('每种都给出非空的说明文案', () => {
    for (const v of all()) {
      expect(styleOf(v).desc.length).toBeGreaterThan(0)
      expect(styleOf(v).labelEn.length).toBeGreaterThan(0)
    }
  })

  // 不变量 INV-8：补救失败必须能被看见，不能伪装成成功。
  it('Unresolved 存在且语义是"未补救"而非成功', () => {
    const s = styleOf('Unresolved')
    expect(s.label).toBe('未补救')
    expect(s.label).not.toMatch(/正常|成功/)
  })

  // 禁止裸色值：只允许 Tailwind / daisyUI 的语义色 class。
  it('颜色只用语义 class，不含裸色值', () => {
    for (const v of all()) {
      const c = styleOf(v).color
      expect(c).toMatch(/^(text|badge|bg)-/)
      expect(c).not.toMatch(/#[0-9a-f]{3,8}|rgb|oklch/i)
    }
  })
})
