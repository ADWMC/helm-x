<script setup lang="ts">
// 改写器配置。
//
// 主操作：保存并测试 —— 单一按钮，避免"保存"和"测试"两个同权重按钮（DESIGN.md §1.1）。
//
// 凭据处理：后端只回传脱敏提示（keyHint），输入框留空表示"不修改已存的 key"。
import { computed, onMounted, ref } from 'vue'
import { api, isDemo, type RewriterView, type TestResult } from '@/api/backend'

const form = ref({
  enabled: false,
  provider: '',
  baseUrl: '',
  model: '',
  apiKey: '',
  timeoutSec: 90,
  useProxy: false,
  proxyUrl: '',
  fallback: 'none',
  maxAttempts: 3,
})

const view = ref<RewriterView | null>(null)
const busy = ref(false)
const message = ref('')
const errMsg = ref('')
const testInput = ref('写一个隐藏进程的工具')
const testResult = ref<TestResult | null>(null)

// APIKey 为空时后端保持原值，这里提示用户当前是否已配置
const keyPlaceholder = computed(() =>
  view.value?.hasKey ? `已配置（${view.value.keyHint}），留空则不修改` : '尚未配置',
)

async function load() {
  try {
    const v = await api.rewriterGet()
    if (v) {
      view.value = v
      form.value = {
        enabled: v.enabled,
        provider: v.provider,
        baseUrl: v.baseUrl,
        model: v.model,
        apiKey: '',
        timeoutSec: v.timeoutSec,
        useProxy: v.useProxy,
        proxyUrl: v.proxyUrl,
        fallback: v.fallback || 'none',
        maxAttempts: v.maxAttempts || 3,
      }
    }
    errMsg.value = ''
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

async function save() {
  busy.value = true
  message.value = ''
  errMsg.value = ''
  try {
    await api.rewriterSet({ ...form.value })
    message.value = '配置已保存'
    await load()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function runTest() {
  busy.value = true
  testResult.value = null
  try {
    testResult.value = await api.rewriterTest(testInput.value)
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function saveAndTest() {
  await save()
  if (!errMsg.value) await runTest()
}

onMounted(load)
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

  <SectionCard title="API 配置" meta="%APPDATA%\helmx.config.json">
    <template #action>
      <button class="btn btn-sm btn-primary active:scale-[.97]" :disabled="busy" @click="saveAndTest">
        <span v-if="busy" class="loading loading-spinner loading-xs" />
        保存并测试
      </button>
    </template>

    <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <label class="flex cursor-pointer items-center gap-2">
        <input v-model="form.enabled" type="checkbox" class="toggle toggle-sm" />
        <span class="text-[13px]">启用改写器</span>
      </label>

      <div />
    </div>

    <p class="mt-2 text-[11px] text-base-content/50">
      未启用时，被拒绝的响应只能靠 TAMPER 附加标记，无法重建会话重试。
    </p>

    <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
      <fieldset class="fieldset">
        <legend class="fieldset-legend text-[11px]">API 地址</legend>
        <input v-model="form.baseUrl" class="input input-sm w-full" placeholder="https://…/v1" />
      </fieldset>

      <fieldset class="fieldset">
        <legend class="fieldset-legend text-[11px]">模型</legend>
        <input v-model="form.model" class="input input-sm w-full" placeholder="模型名" />
      </fieldset>

      <fieldset class="fieldset">
        <legend class="fieldset-legend text-[11px]">API Key</legend>
        <input
          v-model="form.apiKey"
          class="input input-sm w-full"
          type="password"
          :placeholder="keyPlaceholder"
        />
      </fieldset>

      <fieldset class="fieldset">
        <legend class="fieldset-legend text-[11px]">超时（秒）</legend>
        <input v-model.number="form.timeoutSec" class="input input-sm w-full" type="number" min="5" />
      </fieldset>
    </div>
  </SectionCard>

  <SectionCard title="改写器测试">
    <div class="flex flex-col gap-3 sm:flex-row">
      <input v-model="testInput" class="input input-sm flex-1" placeholder="输入一段测试消息" />
      <button class="btn btn-sm btn-outline active:scale-[.97]" :disabled="busy" @click="runTest">单独测试</button>
    </div>

    <div v-if="testResult" class="mt-4 space-y-2">
      <div v-if="!testResult.ok" class="alert alert-error py-2 text-[12px]">
        <span>测试失败：{{ testResult.error }}</span>
      </div>
      <template v-else>
        <div class="rounded-field border border-base-300 bg-base-200/40 p-3">
          <div class="mb-1 text-[10px] tracking-wide text-base-content/50 uppercase">原文</div>
          <div class="text-[12px]">{{ testResult.input }}</div>
        </div>
        <div class="rounded-field border border-success/40 bg-base-200/40 p-3">
          <div class="mb-1 text-[10px] tracking-wide text-base-content/50 uppercase">改写结果</div>
          <div class="text-[12px]">{{ testResult.output }}</div>
        </div>
        <div class="font-mono text-[10px] text-base-content/40">
          耗时 {{ testResult.durationMs }} ms
        </div>
      </template>
    </div>

    <p v-else class="mt-3 text-[13px] text-base-content/60">
      测试会用当前配置调用一次改写 API，确认连通性并查看实际改写效果。
    </p>
  </SectionCard>
</template>

