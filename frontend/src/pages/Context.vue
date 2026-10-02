<script setup lang="ts">
// 上下文：Codex 请求压缩设置。
//
// 【变更记录】本页原含 Context Gardener 的"启用裁剪 / 工具输出裁剪阈值"两项。
// 实测该功能在当前 codex 协议下**完全空转**（裁剪目标 type 不存在），
// 用户决策删除，故两个输入项已移除。
//
// 保留的三项是 **codex 自身的压缩设置**，由 helm-x 注入到 config.toml 顶层，
// 与已删除的 Context Gardener 无关（见 codexcfg.ContextDefaultKeys）：
//   tool_output_token_limit / model_auto_compact_token_limit
//   / model_auto_compact_token_limit_scope
//
// 这三项存在 config.toml 里由 codex 自己读取执行，本页只做展示与说明 ——
// 直接改它们需要重写用户的配置文件，风险高于收益。
import { computed, onMounted, ref } from 'vue'
import { api, isDemo, type CodexState } from '@/api/backend'

const state = ref<CodexState | null>(null)
const errMsg = ref('')

// 与 codexcfg.ContextDefaultKeys 保持一致
const KEYS = [
  {
    key: 'tool_output_token_limit',
    desc: '单个工具输出的 token 上限。超过时由 codex 自己裁剪。',
  },
  {
    key: 'model_auto_compact_token_limit',
    desc: '上下文达到该 token 数时触发自动压缩。',
  },
  {
    key: 'model_auto_compact_token_limit_scope',
    desc: '压缩计算范围。body_after_prefix 表示只统计前缀之后的部分。',
  },
]

const injected = computed(() => state.value?.injected ?? false)
const missing = computed(() => state.value?.missingKeys ?? [])
const hasKey = (k: string) => !missing.value.includes(k)

onMounted(async () => {
  try {
    state.value = await api.configState()
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

  <SectionCard title="Codex 请求压缩" :meta="injected ? '已注入' : '未注入'">
    <div class="space-y-2">
      <div
        v-for="k in KEYS"
        :key="k.key"
        class="flex items-start gap-3 rounded-field border border-base-300 bg-base-200/30 px-3 py-2.5"
      >
        <span
          class="mt-0.5 w-11 shrink-0 font-mono text-[10px]"
          :class="hasKey(k.key) ? 'text-success' : 'text-warning'"
        >
          {{ hasKey(k.key) ? '已设置' : '缺失' }}
        </span>
        <div class="min-w-0 flex-1">
          <div class="font-mono text-[12px]">{{ k.key }}</div>
          <div class="mt-0.5 text-[12px] text-base-content/60">{{ k.desc }}</div>
        </div>
      </div>
    </div>

    <p v-if="!injected && state?.found" class="mt-3 text-[12px] text-warning">
      部分键缺失。在「服务」页执行「注入配置」即可补齐。
    </p>
    <p v-else-if="!state?.found" class="mt-3 text-[12px] text-base-content/60">
      未找到 codex 配置。请先运行一次 codex，或设置 CODEX_HOME 环境变量。
    </p>

    <!-- 明确说明已删除的功能，避免用户找不到 -->
    <div class="mt-4 rounded-field border border-base-300 bg-base-200/20 px-3 py-2.5">
      <div class="text-[11px] tracking-wide text-base-content/50 uppercase">已移除的功能</div>
      <p class="mt-1 text-[12px] text-base-content/60">
        「上下文裁剪」（Context Gardener）已从 helm-x 移除。实测它在当前 codex 协议下
        找不到裁剪目标，运行时什么都不做，因此不再提供相关开关。
      </p>
    </div>

    <p v-if="state?.found" class="mt-3 font-mono text-[10px] break-all text-base-content/40">
      {{ state.configPath }}
    </p>
  </SectionCard>
</template>
