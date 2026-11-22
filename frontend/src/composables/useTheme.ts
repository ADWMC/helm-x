// useTheme —— 主题偏好（跟随系统 / 浅色 / 深色）。
//
// 为什么前端管而不是后端：主题是纯渲染层状态，存 localStorage 即可跨重启保留；
// 走一次 Go 往返只会让"切主题"多 20ms 且引入配置读写失败面。
//
// 实现约定：只切 <html data-theme>，令牌全在 style.css 的主题块里。
// 两套 token 都实测达标（python tools/contrast.py --theme light|dark）。
//
// 默认是 system：首次启动跟随 Windows 的主题，用户显式选过才覆盖。
import { computed, ref } from 'vue'

export type ThemePref = 'light' | 'dark' | 'system'

const KEY = 'helmx.theme'

const pref = ref<ThemePref>(read())
const systemDark = ref(false)

function read(): ThemePref {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {
    // localStorage 不可用（隐私模式）：用默认值
  }
  // 默认跟随系统：Windows 深色模式下首屏就该是深色，不该强制浅色
	return 'system'
}

function systemPrefersDark(): boolean {
  try {
    return window.matchMedia('(prefers-color-scheme: dark)').matches
  } catch {
    return false
  }
}

/** 计算实际生效的主题。 */
export const effective = computed<'light' | 'dark'>(() =>
  pref.value === 'system' ? (systemDark.value ? 'dark' : 'light') : pref.value,
)

function apply() {
  document.documentElement.dataset.theme = effective.value
}

/** 设置偏好并立即应用（持久化）。 */
export function setTheme(v: ThemePref) {
  pref.value = v
  try {
    localStorage.setItem(KEY, v)
  } catch {
    // 忽略：切主题失败不该影响使用
  }
  apply()
}

/** 在应用启动时调用一次：读偏好、监听系统主题变化。 */
export function initTheme() {
  systemDark.value = systemPrefersDark()
  apply()
  try {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    mq.addEventListener('change', (e) => {
      systemDark.value = e.matches
      if (pref.value === 'system') apply()
    })
  } catch {
    // 老 WebView 没有 addEventListener：跟随系统这一档退化为启动时快照
  }
}

/** 供设置页读取当前偏好。 */
export function useTheme() {
  return { pref, effective, setTheme }
}
