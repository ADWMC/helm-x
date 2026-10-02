<script setup lang="ts">
// 实时日志（DESIGN-SPEC §5.3）。
// 关键行为：用户上滚自动暂停跟随，且暂停期间新行进缓冲不丢
// —— 这正是原版轮询模式做不到、也是引入事件推送的理由之一（DESIGN.md §3.3）。
import { computed, nextTick, ref, watch } from 'vue'

export interface LogLine {
  ts: string
  level?: 'info' | 'warn' | 'error'
  text: string
}

const props = withDefaults(
  defineProps<{
    lines: LogLine[]
    /** 上限，超出丢弃最旧 */
    max?: number
  }>(),
  { max: 2000 },
)

const following = ref(true)
const scroller = ref<HTMLElement | null>(null)
/** 暂停期间涌入的行数，用于「已暂停 · N 条新记录」 */
const pending = ref(0)

const shown = computed(() => {
  const all = props.lines
  return all.length > props.max ? all.slice(all.length - props.max) : all
})

const truncated = computed(() => props.lines.length > props.max)

function scrollToBottom() {
  const el = scroller.value
  if (el) el.scrollTop = el.scrollHeight
}

/** 只有用户在底部附近时才继续跟随；否则视为用户要读历史 */
function onScroll() {
  const el = scroller.value
  if (!el) return
  const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24
  following.value = atBottom
  if (atBottom) pending.value = 0
}

function resume() {
  following.value = true
  pending.value = 0
  void nextTick(scrollToBottom)
}

watch(
  () => props.lines.length,
  () => {
    if (following.value) void nextTick(scrollToBottom)
    else pending.value += 1
  },
)

defineExpose({ resume })
</script>

<template>
  <div class="relative">
    <div
      v-if="truncated"
      class="mb-1.5 font-mono text-[10px] text-base-content/50"
    >
      仅显示最近 {{ max }} 条
    </div>

    <pre
      ref="scroller"
      class="max-h-[320px] min-h-[80px] overflow-y-auto rounded-field border border-base-300 bg-base-200/40 p-3.5 font-mono text-[11.5px] leading-[1.6] whitespace-pre-wrap break-all select-text"
      @scroll.passive="onScroll"
    ><template v-for="(l, i) in shown" :key="i"><span
        :class="{
          'text-error': l.level === 'error',
          'text-warning': l.level === 'warn',
          'text-base-content/50': l.level !== 'error' && l.level !== 'warn',
        }"
      >{{ l.ts }}</span> {{ l.text }}
</template><span v-if="!shown.length" class="text-base-content/50">（暂无日志）</span></pre>

    <button
      v-if="!following"
      type="button"
      class="btn btn-xs btn-primary absolute right-3 bottom-3"
      @click="resume"
    >
      已暂停 · {{ pending }} 条新记录
    </button>
  </div>
</template>
