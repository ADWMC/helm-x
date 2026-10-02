<script setup lang="ts">
// 运行日志。
//
// 【相对旧版的改动】旧版靠前端 3 秒轮询 /api/log；新版用事件推送，
// 配 LogStream 的"跟随/暂停"行为（DESIGN.md §3.3）。
import { onMounted, onUnmounted, ref } from 'vue'
import { api, events, isDemo, isDesktop } from '@/api/backend'
import LogStream, { type LogLine } from '@/components/LogStream.vue'

const lines = ref<LogLine[]>([])
const logPath = ref('')
const errMsg = ref('')
let offEvent: (() => void) | null = null

// 把后端的 {ts, level, text} 转成 LogStream 需要的行
function toLine(l: { ts: string; level: string; text: string }): LogLine {
  return {
    ts: l.ts,
    level: (l.level as LogLine['level']) ?? 'info',
    text: l.text,
  }
}

async function load() {
  try {
    const tail = await api.logTail(500)
    lines.value = tail.map(toLine)
    logPath.value = await api.logPath()
    errMsg.value = ''
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

onMounted(async () => {
  await load()

  if (isDesktop) {
    const { Events } = await import('@wailsio/runtime')
    offEvent = Events.On(events.log, (ev: { data: { ts: string; level: string; text: string } }) => {
      if (!ev?.data) return
      lines.value = [...lines.value, toLine(ev.data)]
    }) as unknown as () => void
  }
})

onUnmounted(() => {
  if (offEvent) offEvent()
})
</script>

<template>
  <div v-if="isDemo" class="alert alert-warning mb-4 py-2 text-[12px]">
    <span>浏览器预览模式：Go 后端未连接。</span>
  </div>
  <div v-if="errMsg" class="alert alert-error mb-4 py-2 text-[12px]">
    <span>{{ errMsg }}</span>
  </div>

  <SectionCard title="代理日志" :meta="logPath || ''">
    <template #action>
      <button class="btn btn-xs btn-ghost active:scale-[.97]" @click="load">重新加载</button>
    </template>

    <LogStream :lines="lines" :max="2000" />

    <p class="mt-3 text-[11px] text-base-content/50">
      日志实时推送，无需刷新。手动上滚会自动暂停跟随，暂停期间新记录不会丢失。
    </p>
  </SectionCard>
</template>



