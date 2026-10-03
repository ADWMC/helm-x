<script setup lang="ts">
// 提示词模式选择。
//
// 主操作：应用模式。切换后**下次请求即生效** —— 代理每请求读配置（INV-5），
// 不需要重启（旧版注释里也强调了这一点）。
import { computed, onMounted, ref } from 'vue'
import { api, isDemo, type AppSettings, type PromptModeView } from '@/api/backend'

const modes = ref<PromptModeView[]>([])
const selected = ref('')
const busy = ref(false)
const errMsg = ref('')
const message = ref('')

// ── 注入频率（整体替换式保存，必须先读后写）──
const settings = ref<AppSettings | null>(null)
const every = ref(1)
const everyBusy = ref(false)

const active = computed(() => modes.value.find((m) => m.active))
const current = computed(() => modes.value.find((m) => m.id === selected.value))

async function load() {
  try {
    modes.value = await api.promptModes()
    selected.value = active.value?.id ?? modes.value[0]?.id ?? ''
    errMsg.value = ''
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

async function loadSettings() {
  try {
    settings.value = await api.settings()
    every.value = Math.max(1, Math.floor(Number(settings.value.inject_every ?? 1)) || 1)
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

async function saveEvery() {
  if (!settings.value) return
  everyBusy.value = true
  message.value = ''
  errMsg.value = ''
  try {
    const n = Math.max(1, Math.floor(Number(every.value)) || 1)
    await api.saveSettings({ ...settings.value, inject_every: n })
    every.value = n
    message.value = '已保存，下次请求即生效'
    await loadSettings()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    everyBusy.value = false
  }
}

async function apply() {
  if (!selected.value) return
  busy.value = true
  message.value = ''
  errMsg.value = ''
  try {
    await api.promptSet(selected.value)
    message.value = '已切换，下次请求即生效'
    await load()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  void load()
  void loadSettings()
})
</script>

<template>
  <div v-if="isDemo" class="alert alert-warning mb-4 py-2 text-[12px]">
    <span>浏览器预览模式：Go 后端未连接。</span>
  </div>
  <div v-if="message" class="alert alert-success mb-4 py-2 text-[12px]">
    <span>{{ message }}</span>
  </div>
  <div v-if="errMsg" class="alert alert-error mb-4 py-2 text-[12px]">
    <span>{{ errMsg }}</span>
  </div>

  <SectionCard title="提示词模式" :meta="active ? `当前：${active.name}` : ''">
    <template #action>
      <button
        class="btn btn-sm btn-primary active:scale-[.97]"
        :disabled="busy || !selected || selected === active?.id"
        @click="apply"
      >
        <span v-if="busy" class="loading loading-spinner loading-xs" />
        应用模式
      </button>
    </template>

    <div v-if="modes.length" class="space-y-2">
      <label
        v-for="m in modes"
        :key="m.id"
        class="flex cursor-pointer items-start gap-3 rounded-field border p-3 transition-colors"
        :class="
          selected === m.id
            ? 'border-primary bg-base-200'
            : 'border-base-300 bg-base-100 hover:bg-base-200/60'
        "
      >
        <input v-model="selected" type="radio" :value="m.id" class="radio radio-sm mt-0.5" />
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <span class="text-[13px] font-medium">{{ m.name }}</span>
            <span v-if="m.active" class="badge badge-xs badge-primary">使用中</span>
            <span v-if="m.default" class="badge badge-xs badge-ghost">默认</span>
          </div>
          <div class="mt-0.5 text-[12px] text-base-content/60">{{ m.description }}</div>
          <div class="mt-1 font-mono text-[10px] text-base-content/40">
            {{ m.id }} · {{ (m.bytes / 1024).toFixed(1) }} KB
          </div>
        </div>
      </label>
    </div>

    <SkeletonRows v-else :rows="5" :cols="2" />

    <p class="mt-3 text-[11px] text-base-content/50">
      提示词按下方「注入频率」插入到 input 数组的第一条（已实测被上游接受且生效）。
    </p>
  </SectionCard>

  <SectionCard title="注入频率" meta="按会话计数">
    <div class="flex flex-wrap items-center gap-3">
      <label class="flex items-center gap-2 text-[13px]">
        每
        <input
          v-model.number="every"
          type="number"
          min="1"
          step="1"
          class="input input-sm w-20 text-center"
        />
        次请求注入 1 次
      </label>
      <button class="btn btn-sm btn-outline active:scale-[.97]" :disabled="everyBusy" @click="saveEvery">
        <span v-if="everyBusy" class="loading loading-spinner loading-xs" />
        保存
      </button>
    </div>
    <p class="mt-3 text-[11px] text-base-content/50">
      按会话（Session-Id）各自计数：每个会话的首次请求注入，之后每 N 次注入 1 次。
      N=1 表示每次都注入。只有会注入的请求才计数；未注入的请求原样转发，
      请求列表里不显示「注入」标记。
    </p>
  </SectionCard>

  <SectionCard v-if="current" title="预览" :meta="current.name">
    <pre
      class="max-h-[320px] overflow-y-auto rounded-field border border-base-300 bg-base-200/40 p-3.5 font-mono text-[11.5px] leading-[1.6] whitespace-pre-wrap"
      >{{ current.preview }}</pre
    >
    <p class="mt-2 text-[11px] text-base-content/50">
      仅显示开头部分，完整内容 {{ current.bytes }} 字节。
    </p>
  </SectionCard>
</template>


