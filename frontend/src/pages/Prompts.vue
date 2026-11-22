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

// ── 部署语义：同一段文本放哪、覆盖什么（2026-11-22 随主臂迁入）──
const deploy = ref('insert')
const DEPLOYS = [
  { value: 'insert', label: '插入 system 消息', hint: '往 input 数组插一条 system（历史行为，实测生效）' },
  { value: 'append', label: '追加到 instructions', hint: '保留官方 base，指令贴在其后（零额外暴露位）' },
  {
    value: 'replace-base',
    label: '整段替换 instructions',
    hint: '覆盖官方 base_instructions（21.3KB）。主臂模式必须用这一档，否则实测结论不成立',
  },
] as const
const deployBusy = ref(false)

// ── 注入频率（整体替换式保存，必须先读后写）──
const settings = ref<AppSettings | null>(null)
const every = ref(1)
const everyBusy = ref(false)

const active = computed(() => modes.value.find((m) => m.active))
const current = computed(() => modes.value.find((m) => m.id === selected.value))

async function load() {
  try {
    modes.value = await api.promptModes()
    deploy.value = await api.promptGetDeploy()
    selected.value = active.value?.id ?? modes.value[0]?.id ?? ''
    errMsg.value = ''
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

/** 切换部署语义（立即生效，下次请求即用） */
async function saveDeploy(next: string) {
  if (next === deploy.value) return
  deployBusy.value = true
  message.value = ''
  errMsg.value = ''
  try {
    await api.promptSetDeploy(next)
    deploy.value = next
    message.value = '部署语义已切换，下次请求即生效'
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    deployBusy.value = false
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
    // 主臂类模式以"整段替换"为实测前提：先把部署语义对齐，再落模式，
    // 避免"选了个跑不对的模式却看不出来"。
    if (current.value?.deploy === 'replace-base' && deploy.value !== 'replace-base') {
      await api.promptSetDeploy('replace-base')
      deploy.value = 'replace-base'
    }
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
            <span v-if="m.deploy === 'replace-base'" class="badge badge-xs badge-warning">整段替换</span>
          </div>
          <div class="mt-0.5 text-[12px] text-base-content/60">{{ m.description }}</div>
          <div class="mt-1 flex flex-wrap items-center gap-2 font-mono text-[11px] text-base-content/60">
            <span>{{ m.id }} · {{ (m.bytes / 1024).toFixed(1) }} KB</span>
            <span v-if="m.models" class="badge badge-xs badge-outline">{{ m.models }}</span>
          </div>
        </div>
      </label>
    </div>

    <SkeletonRows v-else :rows="5" :cols="2" />
  </SectionCard>

  <SectionCard title="部署语义" :meta="DEPLOYS.find((d) => d.value === deploy)?.label">
    <div class="space-y-2">
      <label
        v-for="d in DEPLOYS"
        :key="d.value"
        class="flex cursor-pointer items-start gap-3 rounded-field border p-3 transition-colors"
        :class="
          deploy === d.value
            ? 'border-primary bg-base-200'
            : 'border-base-300 bg-base-100 hover:bg-base-200/60'
        "
      >
        <input
          type="radio"
          class="radio radio-sm mt-0.5"
          :value="d.value"
          :checked="deploy === d.value"
          :disabled="deployBusy"
          @change="saveDeploy(d.value)"
        />
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <span class="text-[13px] font-medium">{{ d.label }}</span>
            <span class="badge badge-xs badge-outline font-mono">{{ d.value }}</span>
            <span v-if="d.value === deploy" class="badge badge-xs badge-primary">使用中</span>
          </div>
        </div>
      </label>
    </div>
    <p v-if="deploy === 'replace-base'" class="mt-3 text-[12px] text-warning">
      覆盖官方 base_instructions（21.3KB）。主臂依赖这一档。
    </p>
  </SectionCard>

  <SectionCard title="注入频率" meta="按会话采样">
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
        个会话注入 1 个
      </label>
      <button class="btn btn-sm btn-outline active:scale-[.97]" :disabled="everyBusy" @click="saveEvery">
        <span v-if="everyBusy" class="loading loading-spinner loading-xs" />
        保存
      </button>
    </div>
    <p class="mt-2 text-[12px] text-base-content/70">1 = 每个会话都注入；命中后全程跟随。</p>
  </SectionCard>

  <SectionCard v-if="current" title="预览" :meta="`${current.name} · ${current.bytes} 字节`">
    <pre
      class="max-h-[320px] overflow-y-auto rounded-field border border-base-300 bg-base-200/40 p-3.5 font-mono text-[11.5px] leading-[1.6] whitespace-pre-wrap"
      >{{ current.preview }}</pre
    >
  </SectionCard>
</template>


