<script setup lang="ts">
// 面板容器（DESIGN-SPEC §5.4）。尺寸取原版实测值，不重定（DESIGN.md §7 表）。
// 禁止嵌套 SectionCard（DESIGN.md §1.3：卡片套卡片）。
//
// 【本次改动】标题从 text-xl 降到 13px。
// 原来卡片标题与页面 h1 几乎同尺寸，一屏出现多个等重标题让层级失效
// （redesign-skill 的 Typography 一条：标题要有层级）。
// 现在页面 h1 是唯一的 2xl，卡片标题降到 13px 后层级才成立。
// 同时给卡头加了一层极浅底色，让"卡头/内容"分区不靠一条线硬切。
defineProps<{
  title: string
  /** 右上角元信息，等宽小字 */
  meta?: string
}>()
</script>

<template>
  <section class="card mb-5 overflow-hidden rounded-box">
    <header
      class="flex min-h-12 items-center justify-between gap-3.5 border-b border-[var(--color-hairline-soft)] px-[18px]"
    >
      <h2 class="text-[13px] leading-tight font-semibold tracking-[.01em]">{{ title }}</h2>
      <div class="flex items-center gap-3">
        <span v-if="meta" class="font-mono text-[10px] tracking-[.06em] text-base-content/55">
          {{ meta }}
        </span>
        <slot name="action" />
      </div>
    </header>
    <div class="p-[18px]">
      <slot />
    </div>
  </section>
</template>
