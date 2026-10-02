/**
 * 判定状态 → 样式映射。全项目唯一一处（DESIGN-SPEC §4）。
 *
 * 三条硬约束：
 * 1. dot 用形状而非仅颜色 —— 色盲用户、黑白截图都要能读出状态（web-ui 规则 6）
 *    六种形状必须互不相同。最初 Healthy/Refused/Flagged 都用了 ●，
 *    等于退回"只靠颜色"，被 useVerdictStyle.spec.ts 拦下。
 * 2. **颜色也要能区分语义档位**，不能两两撞色。
 *    原实现 Refused/Unresolved 同为 warning、Flagged/UpstreamFailed 同为 error：
 *    形状虽然不同，但扫一眼颜色会把"已改写"和"未补救"、"已重建"和"上游失败"
 *    混为一谈 —— 前者是"处理成功了"，后者是"没处理好"，语义相反。
 *    现在按 info / warning / error / neutral 四档展开，测试锁死唯一性。
 * 3. 业务组件不得自行判断颜色，只能消费这里的结果
 *
 * 这些字符串是协议的一部分，与 Go 侧 ResponseState.Class 一一对应。
 * 改动前先读 PLAN.md §5.5。
 */
export type Verdict =
  | 'Healthy'
  | 'Refused'
  | 'Flagged'
  | 'UpstreamFailed'
  | 'Malformed'
  | 'Unresolved'

export interface VerdictStyle {
  /** 形状标记。六种必须互不相同，否则退回"只靠颜色"。 */
  dot: string
  /** 文本色 class。只允许用主题语义色，禁止裸色值。六种必须互不相同。 */
  color: string
  /** 中文界面词（术语表见 DESIGN.md §4.2） */
  label: string
  labelEn: string
  /** tooltip 文案：说明这个状态意味着什么 */
  desc: string
}

const VERDICT: Record<Verdict, VerdictStyle> = {
  Healthy: {
    dot: '●', // 实心圆：一切正常
    color: 'text-success',
    label: '正常',
    labelEn: 'Passed',
    desc: '上游响应正常，原样转发',
  },
  Refused: {
    dot: '◍', // 带环实心：已介入且处理成功
    color: 'text-info',
    label: '已改写',
    labelEn: 'Rewritten',
    desc: '命中拒绝规则，TAMPER 已改写',
  },
  Flagged: {
    dot: '◉', // 同心双环：换过会话，上下文已重建
    color: 'text-primary',
    label: '已重建会话',
    labelEn: 'Resent',
    desc: '触发安全标记，已换会话重发',
  },
  UpstreamFailed: {
    dot: '○', // 空心：没有内容
    color: 'text-error',
    label: '上游失败',
    labelEn: 'Upstream failed',
    desc: '未拿到有效响应，已重试',
  },
  Malformed: {
    dot: '◌', // 虚线圈：形态未知
    color: 'text-base-content/60',
    label: '无法解析',
    labelEn: 'Unparsed',
    desc: '响应形态不认识，原样透传',
  },
  Unresolved: {
    dot: '◐', // 半填充：补救未完成
    color: 'text-warning',
    label: '未补救',
    labelEn: 'Unresolved',
    desc: '三级补救均失败，已原样返回模型输出',
  },
}

const ORDER: Verdict[] = [
  'Healthy',
  'Refused',
  'Flagged',
  'UpstreamFailed',
  'Malformed',
  'Unresolved',
]

export function useVerdictStyle() {
  return {
    styleOf: (v: Verdict): VerdictStyle => VERDICT[v],
    all: (): Verdict[] => [...ORDER],
  }
}
