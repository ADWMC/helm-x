<script setup lang="ts">
// 判定状态的唯一渲染入口（DESIGN-SPEC §5.1）。
// 不接受 color / icon / class 之类的样式覆盖 —— 状态长什么样由 useVerdictStyle 决定。
import { computed } from 'vue'
import { useVerdictStyle, type Verdict } from '@/composables/useVerdictStyle'

const props = withDefaults(
  defineProps<{
    verdict: Verdict
    /** 只显示圆点，用于密集表格 */
    dotOnly?: boolean
  }>(),
  { dotOnly: false },
)

const { styleOf } = useVerdictStyle()
const style = computed(() => styleOf(props.verdict))
</script>

<template>
  <!-- dotOnly 时用 aria-label 承载状态，而不是塞 .sr-only 子元素：
       子元素的文本仍会被祖先的 textContent 收集，等于没隐藏。 -->
  <span
    class="inline-flex items-center gap-1.5 font-mono text-[10px] whitespace-nowrap"
    :title="style.desc"
    :aria-label="dotOnly ? style.label : undefined"
  >
    <span :class="style.color" aria-hidden="true">{{ style.dot }}</span>
    <span v-if="!dotOnly" :class="style.color">{{ style.label }}</span>
  </span>
</template>
