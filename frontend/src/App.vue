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

const meta = computed(() => PAGE_META[route.path] ?? { title: '', desc: '' })

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

// 键盘：1–9,0 切页；Esc 关抽屉（DESIGN.md §3.5）
function onKey(e: KeyboardEvent) {
  const el = e.target as HTMLElement | null
  const typing = el && /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)
  if (typing || e.metaKey || e.ctrlKey || e.altKey) return

  if (e.key === 'Escape') {
    drawerOpen.value = false
    return
  }
  if (/^[0-9]$/.test(e.key)) {
    const idx = e.key === '0' ? 9 : Number(e.key) - 1
    const path = HOTKEY_ORDER[idx]
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
        class="app-topbar sticky top-0 z-10 flex min-h-14 items-center justify-between gap-3.5 px-5 md:px-8"
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
          class="nav-item inline-flex items-center gap-2 rounded-full border border-[var(--color-hairline)] px-2.5 py-1 text-[11px]"
          :title="status?.proxy.listen ? `监听 ${status.proxy.listen}` : undefined"
        >
          <span
            class="status-dot size-1.5 rounded-full"
            :class="proxyRunning ? 'is-live' : 'bg-base-content/30'"
            aria-hidden="true"
          />
          <span :class="proxyRunning ? 'text-base-content' : 'text-base-content/55'">
            {{ proxyLabel }}
          </span>
        </RouterLink>
      </header>

      <main id="main" class="flex-1 px-5 pt-6 pb-12 md:px-8 md:pt-8">
        <!-- 页面标题：全页唯一一次 h1。display 类承担负字距+紧行高（§15） -->
        <div class="mb-7">
          <h1 class="display text-[26px] font-semibold text-balance">
            {{ meta.title }}
          </h1>
          <p v-if="meta.desc" class="mt-1.5 max-w-[640px] text-[13px] text-pretty text-base-content/60">
            {{ meta.desc }}
          </p>
        </div>

        <!-- 页面切换：进场弹簧、退场快淡，进出同路径（§7） -->
        <RouterView v-slot="{ Component }">
          <Transition name="page" mode="out-in">
            <component :is="Component" />
          </Transition>
        </RouterView>
      </main>
    </div>
  </div>
</template>
