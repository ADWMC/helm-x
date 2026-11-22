<script setup lang="ts">
// 运行日志。
//
// 【2026-11-22 修复】原实现只用 `proxy:log` 事件推送，且文案写「日志实时推送，无需刷新」。
// 实测本机 WebView2 下事件一条都不到（自检页同因：Go 侧已跑完，前端零行），
// 于是这页在加载后就是死的 —— 而文案还在保证"实时"。现在改成：
//   · 每 2 秒拉一次尾部（页面可见时），仍保留事件作为加速通道；
//   · 文案如实写"每 2 秒刷新"，不再承诺一个没验证过的机制。
import { onMounted, onUnmounted, ref } from 'vue'
import { api, events, isDemo, isDesktop } from '@/api/backend'
import LogStream, { type LogLine } from '@/components/LogStream.vue'

const lines = ref<LogLine[]>([])
const logPath = ref('')
const errMsg = ref('')
let offEvent: (() => void) | null = null
let timer: number | undefined

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

function startPoll() {
  stopPoll()
  timer = window.setInterval(() => {
    if (document.visibilityState === 'visible') void load()
  }, 2000)
}
function stopPoll() {
  if (timer !== undefined) {
    window.clearInterval(timer)
    timer = undefined
  }
}

onMounted(async () => {
  await load()
  startPoll()

  if (isDesktop) {
    const { Events } = await import('@wailsio/runtime')
    offEvent = Events.On(events.log, (ev: { data: { ts: string; level: string; text: string } }) => {
      if (!ev?.data) return
      lines.value = [...lines.value, toLine(ev.data)]
    }) as unknown as () => void
  }
})

onUnmounted(() => {
  stopPoll()
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

    <p class="mt-3 text-[12px] text-base-content/70">
      每 2 秒刷新一次尾部 500 行（页面不可见时暂停）。手动上滚会自动暂停跟随，暂停期间新记录不会丢失。
    </p>
  </SectionCard>
</template>
