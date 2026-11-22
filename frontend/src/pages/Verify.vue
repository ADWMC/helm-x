<script setup lang="ts">
// 自检页。
//
// 【2026-11-22 修复】原实现靠 `proxy:verify` 事件推送逐项结果；实测本机
// WebView2 下**一条事件都没到**（Go 侧日志已写「自检完成：0 项失败」，
// 前端零行、永远停在"执行中"）。改为轮询服务端状态：
//   - 结果存在 Go 侧（VerifyService.Checks），不依赖无法验证的通道；
//   - 切走再回来还在（原来的 rows 是组件局部状态，切页即丢）；
//   - 仍然是逐项出现，因为每 350ms 拉一次增量。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, isDemo, type VerifyProgress } from '@/api/backend'

const rows = ref<VerifyProgress[]>([])
const running = ref(false)
const total = ref(0)
const failed = ref(0)
const errMsg = ref('')
let timer: number | undefined

const summary = computed(() => {
  if (running.value) return `执行中… ${rows.value.length}/${total.value || '?'}`
  if (!rows.value.length) return ''
  return failed.value ? `${failed.value} 项未通过` : '全部通过'
})

async function refresh() {
  try {
    const st = await api.verifyChecks()
    rows.value = st.items ?? []
    running.value = st.running
    total.value = st.total
    failed.value = st.items.filter((r) => !r.ok && !r.skip).length
    errMsg.value = ''
    if (!st.running) stopPoll()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
    stopPoll()
  }
}

function startPoll() {
  stopPoll()
  timer = window.setInterval(refresh, 350)
}
function stopPoll() {
  if (timer !== undefined) {
    window.clearInterval(timer)
    timer = undefined
  }
}

async function run(e2e: boolean) {
  rows.value = []
  failed.value = 0
  errMsg.value = ''
  running.value = true
  try {
    await api.verifyRun(e2e)
    startPoll()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
    running.value = false
  }
}

onMounted(async () => {
  await refresh()
  // 从别的页面切回来时，自检可能还在跑：继续跟
  if (running.value) startPoll()
})

onUnmounted(stopPoll)

function mark(r: VerifyProgress): string {
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
          class="mt-px w-11 shrink-0 text-center font-mono text-[11px]"
          :class="r.skip ? 'text-base-content/70' : r.ok ? 'text-success' : 'text-error'"
        >
          {{ mark(r) }}
        </span>
        <div class="min-w-0 flex-1">
          <div class="text-[13px]">{{ r.name }}</div>
          <div v-if="r.detail" class="font-mono text-[12px] break-all text-base-content/70">
            {{ r.detail }}
          </div>
        </div>
        <span class="shrink-0 font-mono text-[11px] text-base-content/70"> {{ r.step }}/{{ r.total }} </span>
      </div>
    </div>

    <p v-else-if="running" class="text-[13px] text-base-content/70">正在执行自检…</p>
    <p v-else class="text-[13px] text-base-content/70">e2e 会真实调用一次 codex，较慢。</p>
  </SectionCard>
</template>
