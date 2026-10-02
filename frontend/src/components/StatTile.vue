<script setup lang="ts">
// 单个指标（DESIGN-SPEC §5.2）。
// 约束：value 不得为空串 —— 无数据时传「未配置」并把下一步动作放进 hint。
// 这是修 V1/V5（首屏大量 "—" 占位）的落点。
withDefaults(
  defineProps<{
    label: string
    value: string
    hint?: string
    tone?: 'default' | 'ok' | 'bad'
  }>(),
  { tone: 'default' },
)
</script>

<template>
  <div class="rounded-box border border-base-300 bg-base-200/40 px-4 py-3.5">
    <span class="block text-[11px] text-base-content/60">{{ label }}</span>
    <strong
      class="mt-2 block text-lg leading-none font-semibold"
      :class="{
        'text-success': tone === 'ok',
        'text-error': tone === 'bad',
      }"
    >
      {{ value }}
    </strong>
    <em
      v-if="hint"
      class="mt-2 block text-[11px] not-italic text-base-content/60"
    >
      {{ hint }}
    </em>
  </div>
</template>
