<script setup lang="ts">
// QA 帮助。
//
// 【相对旧版的改动】旧版首访会弹一个模态拦截（"你遇到了错误，是否查看 QA?"，
// 选项是"A. 是的 / B. 好的" —— 两个同义选项）。DESIGN.md §7.3 已删除该模态：
// 应用不主动弹窗，QA 入口常驻侧边栏。
//
// 云更新（GitHub → 缓存 → 内置）尚未实现，来源会如实标明，不假装已联网。
import { computed, onMounted, ref } from 'vue'
import { api, isDemo, type QAView } from '@/api/backend'

const data = ref<QAView | null>(null)
const search = ref('')
const errMsg = ref('')
const open = ref<Set<number>>(new Set())

const filtered = computed(() => {
  const items = data.value?.items ?? []
  const q = search.value.trim().toLowerCase()
  if (!q) return items
  return items.filter(
    (it) => it.question.toLowerCase().includes(q) || it.answer.toLowerCase().includes(q),
  )
})

function toggle(i: number) {
  const s = new Set(open.value)
  if (s.has(i)) s.delete(i)
  else s.add(i)
  open.value = s
}

onMounted(async () => {
  try {
    data.value = await api.qa()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
})
</script>

<template>
  <div v-if="isDemo" class="alert alert-warning mb-4 py-2 text-[12px]">
    <span>浏览器预览模式：Go 后端未连接。</span>
  </div>
  <div v-if="errMsg" class="alert alert-error mb-4 py-2 text-[12px]">
    <span>{{ errMsg }}</span>
  </div>

  <SectionCard title="常见问题" :meta="data ? `来源：${data.source} · ${data.items.length} 条` : ''">
    <template #action>
      <input v-model="search" class="input input-xs w-40" placeholder="搜索" type="search" />
    </template>

    <div v-if="filtered.length" class="space-y-2">
      <div
        v-for="(it, i) in filtered"
        :key="i"
        class="rounded-field border border-base-300 bg-base-100"
      >
        <button
          class="flex w-full items-center justify-between gap-3 px-4 py-3 text-left"
          @click="toggle(i)"
        >
          <span class="text-[13px] font-medium">{{ it.question }}</span>
          <span class="shrink-0 font-mono text-[11px] text-base-content/40">
            {{ open.has(i) ? '−' : '+' }}
          </span>
        </button>
        <div v-if="open.has(i)" class="border-t border-base-300 px-4 py-3">
          <p class="text-[12.5px] leading-[1.7] whitespace-pre-wrap text-base-content/80">
            {{ it.answer }}
          </p>
        </div>
      </div>
    </div>

    <p v-else-if="data" class="text-[13px] text-base-content/60">
      没有匹配「{{ search }}」的条目。
    </p>
    <SkeletonRows v-else :rows="5" :cols="1" />

    <p v-if="data" class="mt-3 text-[11px] text-base-content/50">
      版本 {{ data.version }} · 更新于 {{ data.updatedAt || '未知' }}。
      云端更新尚未实现，当前使用内置内容。
    </p>
  </SectionCard>
</template>

