<script setup lang="ts">
// 应用骨架（DESIGN-SPEC §5.7）。
// 两段式 grid，沿用原版 .app-shell（dashboard.html:75）—— 没有横向底栏。
//
// 【本次改动】
//   1. 补 skip-link（键盘用户第一个 Tab 落点，redesign-skill 列为必备）
//   2. 顶栏状态改为**实时**（原来是一个写死"代理未启动"的 span，永不更新）
//   3. 文本字符 ☰ / ● → 矢量图标
//   4. 语义标签：侧栏已是 <aside>，此处补 <main id="main">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import SidebarNav from '@/components/SidebarNav.vue'
import Icon from '@/components/Icon.vue'
import { HOTKEY_ORDER, PAGE_META } from '@/router/nav'
import { api, events, isDesktop, type StatusView } from '@/api/backend'

const route = useRoute()
const router = useRouter()
const drawerOpen = ref(false)

const meta = computed(() => PAGE_META[route.path] ?? { title: '' })
// 版本号单一来源：vite define 注入（frontend/package.json）
declare const __APP_VERSION__: string
const version = typeof __APP_VERSION__ === 'string' ? __APP_VERSION__ : '0.0.0'

// ── 顶栏实时状态 ──
const status = ref<StatusView | null>(null)
let offEvent: (() => void) | null = null
let timer: ReturnType<typeof setInterval> | null = null

const proxyRunning = computed(() => status.value?.proxy.running ?? false)
const proxyLabel = computed(() => {
  if (!status.value) return '读取中'
  return proxyRunning.value ? '代理运行中' : '代理已停止'
})

async function refreshStatus() {
  try {
    status.value = await api.status()
  } catch {
    // 状态读取失败不应打断使用；顶栏保持上一次的值
  }
}

// 键盘：1–8 切页，0 回运行日志；Esc 关抽屉（DESIGN.md §3.5）
function onKey(e: KeyboardEvent) {
  const el = e.target as HTMLElement | null
  const typing = el && /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)
  if (typing || e.metaKey || e.ctrlKey || e.altKey) return

  if (e.key === 'Escape') {
    drawerOpen.value = false
    return
  }
  if (/^[0-9]$/.test(e.key)) {
    const path = e.key === '0' ? '/logs' : HOTKEY_ORDER[Number(e.key) - 1]
    if (path) void router.push(path)
  }
}

onMounted(async () => {
  window.addEventListener('keydown', onKey)
  await refreshStatus()

  if (isDesktop) {
    const { Events } = await import('@wailsio/runtime')
    offEvent = Events.On(events.status, () => {
      void refreshStatus()
    }) as unknown as () => void
  }
  // 兜底轮询：事件偶发丢失时（如窗口刚恢复）也能回到正确状态
  timer = setInterval(refreshStatus, 5000)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKey)
  if (offEvent) offEvent()
  if (timer) clearInterval(timer)
})
</script>

<template>
  <a href="#main" class="skip-link btn btn-sm btn-primary">跳到主内容</a>

  <div class="grid min-h-dvh grid-cols-1 md:grid-cols-[236px_minmax(0,1fr)]">
    <SidebarNav :open="drawerOpen" @navigate="drawerOpen = false" />

    <!-- 移动端遮罩 -->
    <div
      v-if="drawerOpen"
      class="fixed inset-0 z-20 bg-black/40 md:hidden"
      @click="drawerOpen = false"
    />

    <div class="flex min-h-dvh min-w-0 flex-col">
      <!-- topbar：只放汉堡 + 面包屑 + 全局状态。不得与页面 h1 同字（修 V3） -->
      <header
        class="app-topbar sticky top-0 z-10 flex h-10 items-center justify-between gap-3 px-4 md:px-6"
      >
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="btn btn-sm btn-ghost md:hidden"
            aria-label="打开导航"
            :aria-expanded="drawerOpen"
            @click="drawerOpen = !drawerOpen"
          >
            <Icon name="prompts" :size="16" />
          </button>
          <nav class="text-[13px] text-base-content/60" aria-label="面包屑">
            <span>概览</span>
            <span class="mx-1.5">/</span>
            <span class="text-base-content">{{ meta.title }}</span>
          </nav>
        </div>

        <!-- 实时代理状态：颜色 + 图标 + 文字三重表达（不只靠颜色） -->
        <RouterLink
          to="/services"
          class="nav-item inline-flex items-center gap-2 rounded-full border border-[var(--color-hairline)] px-2.5 py-1 text-[12px]"
          :title="status?.proxy.listen ? `监听 ${status.proxy.listen}` : undefined"
        >
          <span
            class="status-dot size-1.5 rounded-full"
            :class="proxyRunning ? 'is-live' : 'bg-base-content/45'"
            aria-hidden="true"
          />
          <span :class="proxyRunning ? 'text-base-content' : 'text-base-content/70'">
            {{ proxyLabel }}
          </span>
        </RouterLink>
      </header>

      <main id="main" class="flex-1 px-4 pt-5 pb-8 md:px-6">
        <!-- 页面标题：全页唯一一次 h1。display 类承担负字距+紧行高（§15） -->
        <div class="mb-4">
          <h1 class="display text-[20px] font-semibold text-balance">
            {{ meta.title }}
          </h1>
        </div>

        <!-- 页面切换暂不加 Transition 包装：enter-from 的 opacity:0
             在 WebView2 异常合成环境下有卡死风险（内容整块不显示）。 -->
        <RouterView />
      </main>

      <!-- 状态栏：只放事实（监听 / 上游 / 请求数 / 版本）。JetBrains 式 24px 一行。 -->
      <footer
        class="app-statusbar mt-auto flex items-center gap-4 px-4 font-mono text-base-content/70 md:px-6"
      >
        <span class="inline-flex items-center gap-1.5">
          <span
            class="size-1.5 rounded-full"
            :class="proxyRunning ? 'bg-success' : 'bg-base-content/45'"
            aria-hidden="true"
          />
          {{ proxyRunning ? status?.proxy.listen : '未监听' }}
        </span>
        <span class="truncate">上游 {{ status?.proxy.upstream || '—' }}</span>
        <span>请求 {{ status?.proxy.requests ?? 0 }}</span>
        <span class="ml-auto">v{{ version }}</span>
      </footer>
    </div>
  </div>
</template>
