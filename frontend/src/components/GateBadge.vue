<script setup lang="ts">
/**
 * GateBadge —— 风险闸门判定徽章。
 *
 * 只显示一档风险（filter 或 compliance 各一枚），形状+颜色双编码，
 * 与 VerdictBadge 并列出现时不会混淆（useGateStyle 有形状冲突的测试）。
 */
import { computed } from 'vue'
import { useGateStyle, type GateRisk } from '@/composables/useGateStyle'

const props = withDefaults(
  defineProps<{
    risk?: GateRisk
    /** 只显示圆点，不带文字（表格里省空间） */
    dotOnly?: boolean
  }>(),
  { risk: 'none', dotOnly: false },
)

const { styleOf } = useGateStyle()
const s = computed(() => styleOf(props.risk))
</script>

<template>
  <span class="inline-flex items-center gap-1 whitespace-nowrap" :title="s.desc">
    <span :class="s.color" aria-hidden="true">{{ s.dot }}</span>
    <span v-if="!dotOnly" class="text-[11px]" :class="s.color">{{ s.label }}</span>
  </span>
</template>
