<script setup lang="ts">
/**
 * 图标组件。
 *
 * 【为何自建而非引库】redesign-skill 的图标审计条目点名 Lucide/Feather
 * 是"AI 默认选择"，建议 Phosphor/Heroicons 或**自建以形成区分度**。
 * 本项目只需要 14 个图标，引入整个库（哪怕 tree-shaken）会带来：
 *   - 一次 npm 安装与版本维护
 *   - 与 daisyUI 的 stroke 风格未必一致
 *   - 打包体积与许可文件
 *
 * 因此按 24×24 网格、1.5px 统一描边自建。stroke-width 集中在一处，
 * 改一处即全局一致（原设计用文本字符 ▦⇄✓ 的问题正是风格无法统一）。
 *
 * 用法：<Icon name="dashboard" />
 */
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    name: IconName
    /** 尺寸，默认 16px（与正文 13px 的视觉重量匹配） */
    size?: number | string
  }>(),
  { size: 16 },
)

/**
 * 24×24 网格上的路径。只存 d，属性由外层 svg 统一给。
 *
 * 只保留**实际被引用**的图标。原先还放了 rules / play / stop / refresh /
 * copy / external / check / alert / chevron 九个「以后可能用到」的图标，
 * 结果一个都没用上 —— 未使用的路径既占体积，也让"当前有哪些图标"
 * 这件事无法从文件本身看出。需要时再加。
 */
const PATHS: Record<string, string> = {
  // 导航（名称与 router/nav.ts 的 NavItem.icon 一一对应，一个页面对一个图标，不复用）
  dashboard: 'M3 3h7v9H3zM14 3h7v5h-7zM14 12h7v9h-7zM3 16h7v5H3z',
  requests: 'M7 3v14m0 0l-3-3m3 3l3-3M17 21V7m0 0l3 3m-3-3l-3 3',
  // 报文：三层叠片（捕获树 = 请求 / 上游 / 响应三层原始包）
  capture: 'M12 3l8 4-8 4-8-4 8-4zM4 12l8 4 8-4M4 17l8 4 8-4',
  verify: 'M9 12.5l2 2 4.5-4.5M12 3a9 9 0 100 18 9 9 0 000-18z',
  services: 'M12 15a3 3 0 100-6 3 3 0 000 6zM12 2v2m0 16v2M4.2 4.2l1.4 1.4m12.8 12.8l1.4 1.4M2 12h2m16 0h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4',
  prompts: 'M4 6h16M4 12h11M4 18h11',
  // 破甲：盾牌 + 勾（对请求做加壳，不是"校验"）
  armor: 'M12 3l7 3v5.5c0 4-2.9 7.2-7 9.5-4.1-2.3-7-5.5-7-9.5V6l7-3zM9 12l2 2 4-4',
  qa: 'M12 3a9 9 0 100 18 9 9 0 000-18zm0 13.5v.01M9.5 9.5a2.5 2.5 0 114 2c-.8.6-1.5 1-1.5 2',
  logs: 'M5 4h14v16H5zm3 4h8M8 12h8M8 16h5',
}

export type IconName = keyof typeof PATHS

const d = computed(() => PATHS[props.name] ?? '')
const px = computed(() => (typeof props.size === 'number' ? `${props.size}px` : props.size))
</script>

<template>
  <svg
    :width="px"
    :height="px"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    class="shrink-0"
  >
    <path :d="d" />
  </svg>
</template>
