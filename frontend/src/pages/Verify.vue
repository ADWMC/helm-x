<script setup lang="ts">
// 自检页。
//
// 【相对旧版的改动】旧版靠前端轮询 /api/zxwn 拿结果；新版用事件推流，
// 每完成一项立刻显示（DESIGN.md §3.2：长任务要可见、可切换页面不丢）。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, events, isDemo, isDesktop, type VerifyProgress } from '@/api/backend'

interface Row extends VerifyProgress {}

const rows = ref<Row[]>([])
const running = ref(false)
const report = ref('')
const errMsg = ref('')
let offEvent: (() => void) | null = null

const failed = computed(() => rows.value.filter((r) => !r.ok && !r.skip).length)
const summary = computed(() => {
  if (running.value) return '执行中…'
  if (!rows.value.length) return ''
  return failed.value ? `${failed.value} 项未通过` : '全部通过'
})

async function run(e2e: boolean) {
  rows.value = []
  report.value = ''
  errMsg.value = ''
  running.value = true
  try {
    await api.verifyRun(e2e)
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
    running.value = false
  }
}

onMounted(async () => {
  if (isDesktop) {
    const { Events } = await import('@wailsio/runtime')
    offEvent = Events.On(events.verify, (ev: { data: Record<string, unknown> }) => {
      const d = ev?.data
      if (!d) return
      if (d.done) {
        running.value = false
        return
      }
      rows.value = [...rows.value, d as unknown as Row]
    }) as unknown as () => void
  }
})

onUnmounted(() => {
  if (offEvent) offEvent()
})

function mark(r: Row): string {
  if (r.skip) return 'SKIP'
  return r.ok ? 'PASS' : 'FAIL'
}
</script>

<template>
  <div v-if="isDemo" class="alert alert-warning mb-4 py-2 text-[12px]">
    <span>浏览器预览模式：Go 后端未连接。</span>
  </div>
  <div v-if="errMsg" class="alert alert-error mb-4 py-2 text-[12px]">
    <span>{{ errMsg }}</span>
  </div>

  <SectionCard title="完整性自检" :meta="summary">
    <template #action>
      <div class="flex gap-2">
        <button class="btn btn-sm btn-primary active:scale-[.97]" :disabled="running" @click="run(false)">
          <span v-if="running" class="loading loading-spinner loading-xs" />
          运行自检
        </button>
        <button class="btn btn-sm btn-outline active:scale-[.97]" :disabled="running" @click="run(true)">
          e2e 验证
        </button>
      </div>
    </template>

    <div v-if="rows.length" class="space-y-1.5">
      <div
        v-for="(r, i) in rows"
        :key="i"
        class="flex items-start gap-3 rounded-field border border-base-300 bg-base-200/30 px-3 py-2"
      >
        <span
          class="mt-px w-11 shrink-0 text-center font-mono text-[10px]"
          :class="r.skip ? 'text-base-content/40' : r.ok ? 'text-success' : 'text-error'"
        >
          {{ mark(r) }}
        </span>
        <div class="min-w-0 flex-1">
          <div class="text-[13px]">{{ r.name }}</div>
          <div v-if="r.detail" class="font-mono text-[11px] break-all text-base-content/60">
            {{ r.detail }}
          </div>
        </div>
        <span class="shrink-0 font-mono text-[10px] text-base-content/40">
          {{ r.step }}/{{ r.total }}
        </span>
      </div>
    </div>

    <p v-else-if="running" class="text-[13px] text-base-content/60">
      正在执行自检，结果会逐项出现在这里…
    </p>
    <p v-else class="text-[13px] text-base-content/60">
      点击「运行自检」检查配置注入、资源完整性与 codex 可达性。
      e2e 验证会实际调用一次 codex，较慢但能确认端到端连通。
    </p>

    <p v-if="running" class="mt-4 text-[11px] text-base-content/50">
      可以切换到其他页面，自检不会中断；结果通过事件推送，切回来仍在。
    </p>
  </SectionCard>
</template>




