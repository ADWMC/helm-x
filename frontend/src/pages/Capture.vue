<script setup lang="ts">
// 报文页 —— 盘上捕获的浏览（与「请求」页互补）：
// 请求页看内存摘要（最近 500 条），这里看**原始字节**：
// 01 改写前 / 02 发往上游 / 03 响应 / 04 判定。
//
// 文案纪律：不写"点击/请按"，动作即按钮文字；空态给原因 + 一个动作。
import { computed, onMounted, ref } from 'vue'
import { api, isDemo, type CaptureFile, type CaptureItem, type CaptureState } from '@/api/backend'

const state = ref<CaptureState | null>(null)
const items = ref<CaptureItem[]>([])
const selected = ref<CaptureItem | null>(null)
const files = ref<CaptureFile[]>([])
const current = ref<{ name: string; body: string } | null>(null)
const busy = ref(false)
const errMsg = ref('')

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

async function loadState() {
  try {
    state.value = await api.captureState()
    errMsg.value = ''
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

async function loadList() {
  try {
    items.value = await api.captureList(200)
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

async function toggle(on: boolean) {
  busy.value = true
  try {
    await api.captureSetEnabled(on)
    await loadState()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function toggleRedact(on: boolean) {
  busy.value = true
  try {
    await api.captureSetRedact(on)
    await loadState()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function openItem(it: CaptureItem) {
  selected.value = it
  current.value = null
  try {
    files.value = await api.captureFiles(it.reqId)
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
    files.value = []
  }
}

async function openFile(f: CaptureFile) {
  if (!selected.value) return
  try {
    current.value = { name: f.name, body: await api.captureRead(selected.value.reqId, f.name) }
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

const usageHint = computed(() => {
  const s = state.value
  if (!s) return ''
  return `${fmtBytes(s.bytes)} · ${s.sessions} 个请求 · 配额 ${s.quotaMb} MB`
})

onMounted(async () => {
  await loadState()
  await loadList()
})
</script>

<template>
  <div v-if="isDemo" class="alert alert-warning mb-4 py-2 text-[12px]">
    <span>浏览器预览模式：Go 后端未连接。</span>
  </div>
  <div v-if="errMsg" class="alert alert-error mb-4 py-2 text-[12px]">
    <span>{{ errMsg }}</span>
  </div>

  <SectionCard title="捕获开关" :meta="usageHint">
    <template #action>
      <button class="btn btn-xs btn-ghost active:scale-[.97]" @click="api.captureOpenDir()">打开目录</button>
    </template>
    <div class="flex flex-wrap items-center gap-4">
      <label class="flex items-center gap-2 text-[13px]">
        <input
          type="checkbox"
          class="checkbox checkbox-sm"
          :checked="state?.enabled ?? false"
          :disabled="busy"
          @change="toggle(($event.target as HTMLInputElement).checked)"
        />
        记录每个请求的原始报文
      </label>
      <label class="flex items-center gap-2 text-[13px]">
        <input
          type="checkbox"
          class="checkbox checkbox-sm"
          :checked="state?.redacted ?? true"
          :disabled="busy || !state?.enabled"
          @change="toggleRedact(($event.target as HTMLInputElement).checked)"
        />
        凭据脱敏（Authorization / Cookie / api-key）
      </label>
    </div>
    <p class="mt-2 text-[12px] text-base-content/70">
      <code class="font-mono">{{ state?.root || '—' }}</code> · 记录保留 7 天 · 关闭不清盘
    </p>
  </SectionCard>

  <SectionCard title="捕获列表" :meta="items.length ? `${items.length} 条` : ''">
    <div v-if="items.length" class="grid grid-cols-1 gap-3 lg:grid-cols-2">
      <div class="max-h-[360px] overflow-y-auto rounded-field border border-base-300">
        <table class="table table-sm">
          <thead>
            <tr class="text-[12px]">
              <th class="w-28">时间</th>
              <th>路径</th>
              <th class="w-24">判定</th>
              <th class="w-24 text-right">大小</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="it in items"
              :key="it.reqId"
              class="cursor-pointer text-[12px]"
              :class="selected?.reqId === it.reqId ? 'bg-base-200' : ''"
              @click="openItem(it)"
            >
              <td class="font-mono text-base-content/60">{{ it.ts.slice(11, 19) }}</td>
              <td class="font-mono">{{ it.path }}</td>
              <td>{{ it.class || '—' }}</td>
              <td class="text-right font-mono text-base-content/60">
                {{ fmtBytes(it.inBytes) }} → {{ fmtBytes(it.outBytes) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="rounded-field border border-base-300">
        <div class="border-b border-base-300 px-3 py-2 font-mono text-[11px] text-base-content/60">
          {{ selected?.reqId || '未选择请求' }}
        </div>
        <ul v-if="files.length" class="max-h-[300px] overflow-y-auto">
          <li v-for="f in files" :key="f.name">
            <button
              class="flex w-full items-center justify-between px-3 py-1.5 text-left text-[12px] hover:bg-base-200"
              :class="current?.name === f.name ? 'bg-base-200' : ''"
              @click="openFile(f)"
            >
              <span class="font-mono">{{ f.name }}</span>
              <span class="font-mono text-base-content/60">{{ fmtBytes(f.size) }}</span>
            </button>
          </li>
        </ul>
        <p v-else class="px-3 py-4 text-[12px] text-base-content/60">
          左侧选一条请求，这里列出它的四个报文文件。
        </p>
      </div>
    </div>

    <p v-else class="text-[13px] text-base-content/60">
      还没有捕获记录。打开上方开关，然后在 codex 里发一条消息；每个请求会落一份报文目录。
    </p>
  </SectionCard>

  <SectionCard v-if="current" :title="`报文内容 · ${current.name}`">
    <pre
      class="max-h-[420px] overflow-auto rounded-field border border-base-300 bg-base-200/40 p-3 font-mono text-[11.5px] leading-[1.6] whitespace-pre-wrap"
      >{{ current.body }}</pre
    >
  </SectionCard>
</template>
