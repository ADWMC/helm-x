<script setup lang="ts">
// 破甲页：请求层改写的两个开关。
//
// 【2026-11-22 精简】原来这页还有一张「改写管线」卡，用三条 bullet 讲
// 提示词注入 / 代号表 / 全文编码各做什么 —— 那是给人解释设计的文字，
// 不是控件。使用者打开这页只需要两个开关和当前状态。
import { computed, onMounted, ref } from 'vue'
import { api, isDemo } from '@/api/backend'

const codebook = ref({ enabled: false, slots: 2 })
const encode = ref(false)
const busy = ref(false)
const errMsg = ref('')
const message = ref('')

const stateLine = computed(
  () =>
    `代号表 ${codebook.value.enabled ? `开 · ${codebook.value.slots} 槽` : '关'} · 编码 ${
      encode.value ? '开' : '关'
    }`,
)

async function load() {
  try {
    codebook.value = await api.codebookGet()
    encode.value = await api.encodeGet()
    errMsg.value = ''
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

async function setCodebook(enabled: boolean, slots: number) {
  busy.value = true
  message.value = ''
  try {
    await api.codebookSet(enabled, slots)
    codebook.value = { enabled, slots }
    message.value = '已保存'
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function setEncode(on: boolean) {
  busy.value = true
  message.value = ''
  try {
    await api.encodeSet(on)
    encode.value = on
    message.value = '已保存'
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="isDemo" class="alert alert-warning mb-4 py-2 text-[12px]">
    <span>浏览器预览模式：Go 后端未连接。</span>
  </div>
  <div v-if="message" class="alert alert-success mb-4 py-2 text-[12px]"><span>{{ message }}</span></div>
  <div v-if="errMsg" class="alert alert-error mb-4 py-2 text-[12px]"><span>{{ errMsg }}</span></div>

  <SectionCard title="代号表" :meta="stateLine">
    <div class="flex flex-wrap items-center gap-4">
      <label class="flex items-center gap-2 text-[13px]">
        <input
          type="checkbox"
          class="checkbox checkbox-sm"
          :checked="codebook.enabled"
          :disabled="busy"
          @change="setCodebook(($event.target as HTMLInputElement).checked, codebook.slots)"
        />
        启用
      </label>
      <label class="flex items-center gap-2 text-[13px]" title="每条命中的请求抽几个片段换成伪词（1–8）">
        每条请求替换
        <input
          :value="codebook.slots"
          type="number"
          min="1"
          max="8"
          class="input input-sm w-16 text-center"
          :disabled="busy || !codebook.enabled"
          @change="setCodebook(codebook.enabled, Number(($event.target as HTMLInputElement).value))"
        />
        个片段
      </label>
    </div>
  </SectionCard>

  <SectionCard title="全文编码" :meta="encode ? '开' : '关'">
    <label class="flex items-center gap-2 text-[13px]">
      <input
        type="checkbox"
        class="checkbox checkbox-sm"
        :checked="encode"
        :disabled="busy"
        @change="setEncode(($event.target as HTMLInputElement).checked)"
      />
      启用
    </label>
    <p class="mt-2 text-[12px] text-base-content/70">与代号表互斥：开启时优先。</p>
  </SectionCard>
</template>
