<script setup lang="ts">
// 请求列表 —— 新增页（原 dashboard 没有）。
//
// 这是 P2/P3/P8 这类问题的**唯一可视证据来源**（PLAN.md N4）：
// 每个请求一行，显示判定结果与补救动作。旧版只能 findstr 日志。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, events, isDemo, isDesktop, type RequestRecord } from '@/api/backend'
import { useVerdictStyle, type Verdict } from '@/composables/useVerdictStyle'
import VerdictBadge from '@/components/VerdictBadge.vue'

const { styleOf, all } = useVerdictStyle()

const rows = ref<RequestRecord[]>([])
const filter = ref<Verdict | 'all'>('all')
const search = ref('')
const errMsg = ref('')
let offEvent: (() => void) | null = null

const filtered = computed(() => {
  let out = rows.value
  if (filter.value !== 'all') out = out.filter((r) => r.class === filter.value)
  if (search.value.trim()) {
    const q = search.value.trim().toLowerCase()
    out = out.filter(
      (r) => r.path.toLowerCase().includes(q) || (r.note || '').toLowerCase().includes(q),
    )
  }
  // 最新在前
  return [...out].reverse()
})

const counts = computed(() => {
  const m: Record<string, number> = {}
  for (const r of rows.value) m[r.class] = (m[r.class] || 0) + 1
  return m
})

async function load() {
  try {
    rows.value = await api.requests(500)
    errMsg.value = ''
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

onMounted(async () => {
  await load()
  if (isDesktop) {
    const { Events } = await import('@wailsio/runtime')
    offEvent = Events.On(events.request, (ev: { data: RequestRecord }) => {
      if (ev?.data) rows.value = [...rows.value, ev.data]
    }) as unknown as () => void
  }
})

onUnmounted(() => {
  if (offEvent) offEvent()
})

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

function fmtTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// 补救动作的中文说明（术语表见 DESIGN.md §4.2）
const ACTION_LABEL: Record<string, string> = {
  none: '—',
  retry: '重建会话重发',
  attach_marker: '附加标记',
  pass_through: '原样返回',
}
</script>

<template>
  <div v-if="isDemo" class="alert alert-warning mb-4 py-2 text-[12px]">
    <span>浏览器预览模式：Go 后端未连接。</span>
  </div>
  <div v-if="errMsg" class="alert alert-error mb-4 py-2 text-[12px]">
    <span>{{ errMsg }}</span>
  </div>

  <!-- 过滤：按判定状态，附带计数 -->
  <div class="mb-4 flex flex-wrap items-center gap-2">
    <button
      class="btn btn-xs active:scale-[.97]"
      :class="filter === 'all' ? 'btn-primary' : 'btn-ghost'"
      @click="filter = 'all'"
    >
      全部 {{ rows.length }}
    </button>
    <button
      v-for="v in all()"
      :key="v"
      class="btn btn-xs active:scale-[.97]"
      :class="filter === v ? 'btn-primary' : 'btn-ghost'"
      :disabled="!counts[v]"
      @click="filter = v"
    >
      <span :class="styleOf(v).color">{{ styleOf(v).dot }}</span>
      {{ styleOf(v).label }} {{ counts[v] || 0 }}
    </button>
    <input
      v-model="search"
      class="input input-sm ml-auto w-48"
      placeholder="过滤路径或备注"
      type="search"
    />
  </div>

  <SectionCard title="请求记录" :meta="filtered.length ? `${filtered.length} 条` : ''">
    <template #action>
      <button class="btn btn-xs btn-ghost active:scale-[.97]" @click="api.clearRequests().then(load)">清空</button>
    </template>

    <div v-if="filtered.length" class="overflow-x-auto">
      <table class="table table-sm table-zebra">
        <thead>
          <tr class="text-[11px]">
            <th class="w-32">时间</th>
            <th class="w-16">会话</th>
            <th>路径</th>
            <th class="w-28">大小</th>
            <th class="w-28">判定</th>
            <th class="w-28">补救</th>
            <th class="w-16 text-right">上游</th>
            <th class="w-20 text-right">耗时</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in filtered" :key="r.id" class="text-[12px]">
            <td class="font-mono text-base-content/60">{{ fmtTime(r.ts) }}</td>
            <td class="font-mono text-[10px] text-base-content/50">
              {{ r.sessionId ? r.sessionId.slice(0, 8) : '—' }}
            </td>
            <td class="font-mono">
              {{ r.path }}
              <span v-if="r.injected" class="badge badge-ghost badge-xs ml-1.5">注入</span>
            </td>
            <td class="font-mono text-base-content/70">
              {{ fmtBytes(r.inBytes) }} → {{ fmtBytes(r.outBytes) }}
            </td>
            <td><VerdictBadge :verdict="r.class" /></td>
            <td class="text-[11px] text-base-content/70">
              {{ ACTION_LABEL[r.action] || r.action }}
            </td>
            <td class="text-right font-mono text-base-content/60">{{ r.upstreamHits }}</td>
            <td class="text-right font-mono text-base-content/60">{{ r.durationMs }} ms</td>
          </tr>
        </tbody>
      </table>
    </div>

    <p v-else class="text-[13px] text-base-content/60">
      没有匹配的请求记录。调整过滤条件，或先在 codex 里发一条消息。
    </p>
  </SectionCard>
</template>




