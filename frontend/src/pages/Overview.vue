<script setup lang="ts">
// 系统总览。
//
// 【设计要点】修原版 dashboard 的三处缺陷（DESIGN.md §0-A）：
//   V1 首屏 3/4 是空占位 → 无数据时显示"下一步做什么"，不用大卡片占位
//   V3 标题重复        → 标题只在内容区出现（App.vue 的 h1）
//   V5 24px 红字抢焦点 → 指标降到 18px，异常才上色
//
// 信息顺序按重要性：代理状态 → 最近请求 → 次要指标。
// 原版把四个等宽指标格放在最显眼位置，而运行时常有三个是空的。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, events, isDemo, isDesktop, type RequestRecord, type StatusView } from '@/api/backend'
import { useVerdictStyle, type Verdict } from '@/composables/useVerdictStyle'
import VerdictBadge from '@/components/VerdictBadge.vue'

const { styleOf } = useVerdictStyle()

const status = ref<StatusView | null>(null)
const requests = ref<RequestRecord[]>([])
const loading = ref(true)
const error = ref('')
let offEvent: (() => void) | null = null

async function refresh() {
  try {
    status.value = await api.status()
    requests.value = await api.requests(20)
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await refresh()

  // 事件驱动，不轮询（原版靠 3 秒轮询，QA 里专门记过这个毛病）
  if (isDesktop) {
    const { Events } = await import('@wailsio/runtime')
    offEvent = Events.On(events.request, () => {
      void refresh()
    }) as unknown as () => void
  }
})

onUnmounted(() => {
  if (offEvent) offEvent()
})

const proxy = computed(() => status.value?.proxy)
const codex = computed(() => status.value?.codex)
const running = computed(() => proxy.value?.running ?? false)

const mainStatusText = computed(() => {
  if (!proxy.value) return '加载中'
  return proxy.value.running ? '运行中' : '已停止'
})

// 首屏要回答的三个问题：配置在哪、有没有注入、codex 是否走本代理
const hints = computed(() => {
  const c = codex.value
  if (!c || !c.found) {
    return ['未找到 codex 配置。请先运行一次 codex，或设置 CODEX_HOME 环境变量。']
  }
  const out: string[] = []
  if (!c.injected) {
    out.push(`配置未注入（缺少 ${c.missingKeys.join('、')}）。在「服务」页执行注入。`)
  }
  if (proxy.value && !proxy.value.proxied) {
    out.push('codex 尚未指向本代理。启动代理后会自动写入。')
  }
  if (out.length === 0) {
    out.push('一切就绪：codex 的请求会经过本代理并注入指令。')
  }
  return out
})

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

function fmtTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  const today = new Date()
  const sameDay =
    d.getFullYear() === today.getFullYear() &&
    d.getMonth() === today.getMonth() &&
    d.getDate() === today.getDate()
  const pad = (n: number) => String(n).padStart(2, '0')
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  if (sameDay) return `${hm}:${pad(d.getSeconds())}`
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hm}`
}
</script>

<template>
  <div v-if="isDemo" class="alert alert-warning mb-4 py-2 text-[12px]">
    <span>浏览器预览模式：Go 后端未连接，以下为占位数据。桌面窗口请在 helmx.exe 中打开。</span>
  </div>

  <div v-if="error" class="alert alert-error mb-4 py-2 text-[12px]">
    <span>读取状态失败：{{ error }}</span>
  </div>

  <!-- 主状态条：一屏唯一说"代理在跑没有"的地方 -->
  <section class="mb-6 rounded-box border border-base-300 bg-base-100 px-4 py-3.5">
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div class="flex items-center gap-3">
        <span
          class="inline-block size-2 shrink-0 rounded-full"
          :class="running ? 'bg-success' : 'bg-base-content/30'"
          aria-hidden="true"
        />
        <strong class="text-[15px]">{{ mainStatusText }}</strong>
        <span v-if="proxy" class="font-mono text-[11px] text-base-content/60">
          {{ proxy.listen }}
        </span>
      </div>
      <div class="text-right font-mono text-[11px] text-base-content/60">
        <div>上游 {{ proxy?.upstream || '—' }}</div>
        <div v-if="proxy">已转发 {{ proxy.requests }} 次</div>
      </div>
    </div>
    <ul class="mt-3 space-y-1 border-t border-base-300 pt-3">
      <li v-for="(h, i) in hints" :key="i" class="text-[12px] text-base-content/70">
        {{ h }}
      </li>
    </ul>
  </section>

  <!-- 最近请求：真实数据优先于空指标 -->
  <SectionCard title="最近请求" :meta="requests.length ? `最近 ${requests.length} 条` : ''">
    <template #action>
      <RouterLink to="/requests" class="btn btn-xs btn-ghost active:scale-[.97]">全部</RouterLink>
    </template>

    <div v-if="requests.length" class="overflow-x-auto">
      <table class="table table-sm">
        <thead>
          <tr class="text-[11px]">
            <th class="w-20">时间</th>
            <th>路径</th>
            <th class="w-28">大小</th>
            <th class="w-28">判定</th>
            <th class="w-20 text-right">耗时</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in requests" :key="r.id" class="text-[12px]">
            <td class="font-mono text-base-content/60">{{ fmtTime(r.ts) }}</td>
            <td class="font-mono">{{ r.path }}</td>
            <td class="font-mono text-base-content/70">
              {{ fmtBytes(r.inBytes) }} → {{ fmtBytes(r.outBytes) }}
            </td>
            <td><VerdictBadge :verdict="r.class" /></td>
            <td class="text-right font-mono text-base-content/60">{{ r.durationMs }} ms</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 空状态必须说下一步做什么 -->
    <SkeletonRows v-else-if="loading" :rows="4" :cols="5" />
    <p v-else class="text-[13px] text-base-content/60">
      还没有请求经过代理。确认 codex 的 <code class="font-mono">base_url</code> 指向
      <code class="font-mono">{{ proxy?.listen || '127.0.0.1:1800' }}/v1</code>
      后，在 codex 里发一条消息。
    </p>
  </SectionCard>

  <!-- 次要指标：降级到下方，18px 而非原版的 24px -->
  <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
    <StatTile
      label="codex 配置"
      :value="codex?.injected ? '已注入' : codex?.found ? '未注入' : '未找到'"
      :hint="codex?.found ? codex.configPath : '未找到 config.toml'"
      :tone="codex?.injected ? 'ok' : codex?.found ? 'bad' : 'default'"
    />
    <StatTile
      label="激活 provider"
      :value="codex?.provider || '未知'"
      :hint="codex?.baseUrl || '配置里没有 base_url'"
    />
    <StatTile
      label="请求判定分布"
      :value="proxy?.requests ? String(proxy.requests) : '0'"
      :hint="
        proxy?.byClass && Object.keys(proxy.byClass).length
          ? Object.entries(proxy.byClass)
              .map(([k, v]) => `${styleOf(k as Verdict).label} ${v}`)
              .join(' · ')
          : '暂无请求'
      "
    />
  </div>
</template>



