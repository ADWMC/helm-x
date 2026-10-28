/**
 * useGateStyle —— 风险闸门三态的视觉映射。
 *
 * 规则与 useVerdictStyle 一致（那条不变量有测试）：
 *   1. 形状先于颜色：闸门三态的 dot 互不相同，且与判定七态（● ◍ ◉ ○ ◌ ◐ ⊘）不重复；
 *   2. 只用语义色 class，禁裸色值；
 *   3. 每态都有独立中文界面词与非空说明。
 */
export type GateRisk = 'none' | 'low' | 'high'

export interface GateStyle {
  /** 形状标记。三态互不相同，且与 Verdict 七态不冲突。 */
  dot: string
  /** 语义色 class。 */
  color: string
  /** 中文界面词。 */
  label: string
  labelEn: string
  /** tooltip：这一档到底意味着什么。 */
  desc: string
}

const GATE: Record<GateRisk, GateStyle> = {
  none: {
    dot: '◆', // 实心菱形：无信号
    color: 'text-success',
    label: '无风险',
    labelEn: 'Clean',
    desc: '本地快筛与模型都未发现风险信号，请求原样通过',
  },
  low: {
    dot: '◈', // 空心菱形：有信号但不确定
    color: 'text-warning',
    label: '待观察',
    labelEn: 'Low',
    desc: '命中部分信号，影子模式下只记录，enforce 模式下可能改写',
  },
  high: {
    dot: '◇', // 细线菱形：判定为有风险
    color: 'text-error',
    label: '有风险',
    labelEn: 'High',
    desc: '判定会被上游过滤或模型拒绝，enforce 模式下按改写重发',
  },
}

const ORDER: GateRisk[] = ['none', 'low', 'high']

export function useGateStyle() {
  const styleOf = (r: GateRisk | undefined): GateStyle => GATE[r ?? 'none'] ?? GATE.none
  const all = (): GateRisk[] => [...ORDER]
  return { styleOf, all }
}
