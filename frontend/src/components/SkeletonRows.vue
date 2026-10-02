<script setup lang="ts">
/**
 * 骨架屏。
 *
 * 【为何新增】redesign-skill 的 Interactivity 一条：
 * "Replace generic circular spinners with skeleton loaders that match the layout shape."
 * 原实现只有 loading-spinner 转圈 —— 转圈不传达"即将出现什么"，
 * 且页面高度会在数据到达时跳变。骨架屏保持布局形状，加载完不抖动。
 */
const props = withDefaults(
  defineProps<{
    /** 骨架行数 */
    rows?: number
    /** 每行的列数（用于表格形状） */
    cols?: number
    /** 紧凑模式：行高更小，用于列表内嵌 */
    dense?: boolean
  }>(),
  { rows: 3, cols: 4, dense: false },
)

const gridStyle = { gridTemplateColumns: `repeat(${props.cols}, minmax(0, 1fr))` }

/**
 * 各列宽度：首列短、末列长，模拟「时间 / 路径 / 大小 / 状态」的真实分布。
 * 等比等宽看起来像占位图，不像内容。
 */
function colWidth(c: number): string {
  if (c === 1) return '55%'
  if (c === props.cols) return '75%'
  return '100%'
}
</script>

<template>
  <div class="animate-pulse space-y-2" role="status" aria-label="正在加载">
    <div v-for="r in rows" :key="r" class="grid gap-3" :style="gridStyle">
      <div
        v-for="c in cols"
        :key="c"
        class="rounded-field bg-base-content/10"
        :class="dense ? 'h-4' : 'h-5'"
        :style="{ width: colWidth(c) }"
      />
    </div>
  </div>
</template>
