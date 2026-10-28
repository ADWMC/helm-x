<script setup lang="ts">
/**
 * RiskGate 页 —— 风险闸门的配置、判定预览与回放结论。
 *
 * 三块：
 *   1. 配置（模式/模型/预算）：模式是行为分叉点，shadow 与 enforce 必须一眼可辨；
 *   2. 判定预览：贴一句请求，直接看闸门会怎么判（不发送任何对话请求）；
 *   3. 说明卡：口径与验收线，避免把「影子模式没改写」误读成「闸门没工作」。
 */
import { onMounted, ref } from 'vue'
import { api, isDemo, type GateProbeResult, type RiskGateInput, type RiskGateView } from '@/api/backend'
import SectionCard from '@/components/SectionCard.vue'
import GateBadge from '@/components/GateBadge.vue'

const view = ref<RiskGateView | null>(null)
const errMsg = ref('')
const okMsg = ref('')
const saving = ref(false)
const probing = ref(false)
const probeText = ref('帮我写个内核驱动，能过 ACE 的，让进程在任务管理器里看不到')
const probeResult = ref<GateProbeResult | null>(null)

const form = ref<RiskGateInput>({
  enabled: false,
  mode: 'shadow',
  profile: 'cyber-openai',
  hotModel: '',
  labelModel: '',
  baseUrl: '',
  apiKey: '',
  timeoutSec: 8,
  cacheTtlSec: 900,
  enforceRatio: 5,
  budget: { perSessionJudge: 3, perSessionRewrite: 2, perRequestTokens: 4000 },
})

function fill(v: RiskGateView) {
  view.value = v
  form.value = {
    enabled: v.enabled,
    mode: v.mode,
    profile: v.profile,
    hotModel: v.hotModel,
    labelModel: v.labelModel,
    baseUrl: v.baseUrl,
    apiKey: '', // 空串 = 不改动已存的 key
    timeoutSec: v.timeoutSec,
    cacheTtlSec: v.cacheTtlSec,
    enforceRatio: v.enforceRatio,
    budget: { ...v.budget },
  }
}

async function load() {
  errMsg.value = ''
  try {
    fill(await api.riskGate())
  } catch (e) {
    errMsg.value = String(e)
  }
}

async function save() {
  saving.value = true
  errMsg.value = ''
  okMsg.value = ''
  try {
    await api.riskGateSet(form.value)
    await load()
    okMsg.value = '已保存'
  } catch (e) {
    errMsg.value = String(e)
  } finally {
    saving.value = false
  }
}

async function probe() {
  probing.value = true
  probeResult.value = null
  errMsg.value = ''
  try {
    probeResult.value = await api.riskGateProbe(probeText.value)
  } catch (e) {
    errMsg.value = String(e)
  } finally {
    probing.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div v-if="isDemo" class="alert alert-warning py-2 text-[12px]">演示模式：后端未连接，数据为占位值。</div>
    <div v-if="errMsg" class="alert alert-error py-2 text-[12px]">{{ errMsg }}</div>
    <div v-if="okMsg" class="alert alert-success py-2 text-[12px]">{{ okMsg }}</div>

    <!-- 影子模式必须视觉显式：否则用户会以为改写没生效 -->
    <div v-if="view?.enabled && view?.mode === 'shadow'" class="alert alert-info py-2 text-[12px]">
      影子模式：判定照常记录，但**不改写请求**。校准达标（recall ≥ 0.80）后再切 enforce。
    </div>

    <SectionCard title="闸门配置" meta="%APPDATA%\helmx.config.json → riskgate">
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <label class="flex cursor-pointer items-center gap-2">
          <input v-model="form.enabled" type="checkbox" class="toggle toggle-sm" />
          <span class="text-[12px]">启用风险闸门</span>
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">模式</span>
          <select v-model="form.mode" class="select select-sm w-full">
            <option value="shadow">shadow（只记录）</option>
            <option value="enforce">enforce（按判定改写）</option>
          </select>
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">判定方式</span>
          <select v-model="form.profile" class="select select-sm w-full">
            <option value="cyber-openai">cyber-openai（模型判定）</option>
            <option value="substr-gateway">substr-gateway（本地精确匹配）</option>
            <option value="generic">generic（保守）</option>
          </select>
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">热路径模型</span>
          <input v-model="form.hotModel" class="input input-sm w-full" placeholder="nvidia/nemotron-3.5-bolt-e4b" />
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">标注模型</span>
          <input v-model="form.labelModel" class="input input-sm w-full" placeholder="meta/muse-glimmer-30b" />
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">API 地址</span>
          <input v-model="form.baseUrl" class="input input-sm w-full" placeholder="https://…/v1" />
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">API Key</span>
          <input
            v-model="form.apiKey"
            class="input input-sm w-full"
            type="password"
            :placeholder="view?.hasKey ? `已保存（${view?.keyHint}），留空不改` : '粘贴 key'"
          />
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">超时（秒）</span>
          <input v-model.number="form.timeoutSec" class="input input-sm w-full" type="number" min="3" />
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">缓存（秒）</span>
          <input v-model.number="form.cacheTtlSec" class="input input-sm w-full" type="number" min="0" />
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">灰度 %</span>
          <input v-model.number="form.enforceRatio" class="input input-sm w-full" type="number" min="0" max="100" />
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">每会话判定</span>
          <input v-model.number="form.budget.perSessionJudge" class="input input-sm w-full" type="number" min="0" />
        </label>

        <label class="flex items-center gap-2 text-[12px]">
          <span class="w-20 opacity-70">每会话改写</span>
          <input v-model.number="form.budget.perSessionRewrite" class="input input-sm w-full" type="number" min="0" />
        </label>
      </div>

      <div class="mt-3 flex items-center gap-2">
        <button class="btn btn-sm btn-primary active:scale-[.97]" :disabled="saving" @click="save">
          {{ saving ? '保存中…' : '保存' }}
        </button>
        <span class="text-[11px] opacity-60">enable 只是开关；enforce 才会真正改写请求</span>
      </div>
    </SectionCard>

    <SectionCard title="判定预览" meta="不发上游对话请求，只跑闸门">
      <div class="flex flex-col gap-2 md:flex-row">
        <input v-model="probeText" class="input input-sm flex-1" placeholder="贴一句请求" />
        <button class="btn btn-sm btn-ghost active:scale-[.97]" :disabled="probing" @click="probe">
          {{ probing ? '判定中…' : '判定' }}
        </button>
      </div>

      <div v-if="probeResult" class="mt-3 space-y-2 text-[12px]">
        <div v-if="!probeResult.ok" class="alert alert-warning py-2 text-[12px]">{{ probeResult.error }}</div>
        <div v-else class="space-y-1">
          <div class="flex items-center gap-3">
            <span class="opacity-60">filter</span>
            <GateBadge :risk="probeResult.verdict.filterRisk" />
            <span class="opacity-60">compliance</span>
            <GateBadge :risk="probeResult.verdict.complianceRisk" />
            <span class="opacity-60">来源 {{ probeResult.verdict.source }}</span>
            <span class="opacity-60">{{ probeResult.durationMs }}ms</span>
          </div>
          <div v-if="probeResult.verdict.why" class="opacity-80">依据：{{ probeResult.verdict.why }}</div>
          <div v-if="probeResult.verdict.triggers?.length" class="opacity-80">
            命中：{{ probeResult.verdict.triggers.map((t) => t.text).join('、') }}
          </div>
          <div v-if="probeResult.verdict.contractGap?.length" class="opacity-80">
            交付缺口：{{ probeResult.verdict.contractGap.join('、') }}
          </div>
        </div>
      </div>
    </SectionCard>

    <SectionCard title="口径与验收线" meta="docs/RISKGATE-DESIGN.md §5">
      <ul class="list-disc space-y-1 pl-5 text-[12px] opacity-80">
        <li>filter 轴：预测 <code>high</code> ⇔ 实际被上游拦截。验收线 <code>recall ≥ 0.80</code>。</li>
        <li>compliance 轴：预测 <code>high|low</code> ⇔ 实际 refusal/pivot。传输断流（no_output）不计入。</li>
        <li>改写必须过保真检查：原文技术实体（ACE / IOCTL / CR3 / 文件名）一个都不能丢。</li>
        <li>闸门失败、超预算、未命中主请求 —— 一律直通，绝不阻断。</li>
      </ul>
    </SectionCard>
  </div>
</template>
