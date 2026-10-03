<script setup lang="ts">
// 服务页：代理启停、自愈守护、配置注入。
//
// 主操作唯一（DESIGN.md §1.1）：启动/停止代理。
// 配置注入属危险操作 → 走 §3.1 流程（列出改哪个文件、怎么撤销）。
import { computed, onMounted, ref } from 'vue'
import { api, isDemo, type AppSettings, type ConfigDiff, type StatusView } from '@/api/backend'

const status = ref<StatusView | null>(null)
const busy = ref('')
const message = ref('')
const errMsg = ref('')

// ── 转发 UA 兜底（整体替换式保存，必须先读后写）──
const settings = ref<AppSettings | null>(null)
const fallbackUA = ref('')
const uaBusy = ref(false)

const showConfirm = ref(false)
const preview = ref<ConfigDiff[]>([])
const pendingAction = ref<'apply' | 'remove'>('apply')

const proxy = computed(() => status.value?.proxy)
const codex = computed(() => status.value?.codex)
const watch = computed(() => status.value?.watch)

async function refresh() {
  status.value = await api.status()
}

async function loadSettings() {
  try {
    settings.value = await api.settings()
    fallbackUA.value = String(settings.value.forward_user_agent ?? '')
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  }
}

async function saveUA() {
  if (!settings.value) return
  uaBusy.value = true
  message.value = ''
  errMsg.value = ''
  try {
    await api.saveSettings({ ...settings.value, forward_user_agent: fallbackUA.value.trim() })
    message.value = 'UA 兜底已保存，下次请求即生效'
    await loadSettings()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    uaBusy.value = false
  }
}

async function toggleProxy() {
  busy.value = 'proxy'
  message.value = ''
  errMsg.value = ''
  try {
    if (proxy.value?.running) {
      await api.stopProxy()
      message.value = '代理已停止，codex 配置已还原'
    } else {
      await api.startProxy()
      message.value = '代理已启动，codex 已指向本代理'
    }
    await refresh()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = ''
  }
}

async function toggleWatch() {
  busy.value = 'watch'
  try {
    if (watch.value?.running) {
      await api.watchStop()
    } else {
      await api.watchStart(60)
    }
    await refresh()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = ''
  }
}

async function askApply() {
  pendingAction.value = 'apply'
  try {
    preview.value = await api.configPreview()
  } catch {
    preview.value = []
  }
  showConfirm.value = true
}

async function askRemove() {
  pendingAction.value = 'remove'
  preview.value = []
  showConfirm.value = true
}

async function confirmDanger() {
  busy.value = 'config'
  showConfirm.value = false
  try {
    const res = pendingAction.value === 'apply' ? await api.configApply() : await api.configRemove()
    message.value = [res.message, ...(res.changed || [])].join(' · ')
    await refresh()
  } catch (e) {
    errMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = ''
  }
}

onMounted(() => {
  void refresh()
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

  <SectionCard title="代理" :meta="proxy?.upstream || ''">
    <div class="flex flex-wrap items-center gap-3">
      <button
        class="btn btn-sm active:scale-[.97]"
        :class="proxy?.running ? 'btn-error btn-outline' : 'btn-primary'"
        :disabled="busy === 'proxy'"
        @click="toggleProxy"
      >
        <span v-if="busy === 'proxy'" class="loading loading-spinner loading-xs" />
        {{ proxy?.running ? '停止代理' : '启动代理' }}
      </button>
      <span class="font-mono text-[11px] text-base-content/60">{{ proxy?.listen }}</span>
      <span class="text-[12px] text-base-content/60">
        {{ proxy?.running ? '运行中' : '已停止' }}
      </span>
    </div>
    <p class="mt-3 text-[12px] text-base-content/60">
      启动时会把 codex 的 base_url 指向本代理；停止时自动还原，codex 回到原始上游。
    </p>
  </SectionCard>

  <SectionCard title="自愈守护" :meta="watch?.running ? '运行中' : '已停止'">
    <div class="flex flex-wrap items-center gap-3">
      <button class="btn btn-sm btn-outline active:scale-[.97]" :disabled="busy === 'watch'" @click="toggleWatch">
        {{ watch?.running ? '停止守护' : '启动守护' }}
      </button>
      <span class="text-[12px] text-base-content/60">
        累计恢复 {{ watch?.restores || 0 }} 次
      </span>
    </div>
    <p v-if="watch?.lastError" class="mt-2 text-[12px] text-error">
      最近异常：{{ watch.lastError }}
    </p>
    <p class="mt-3 text-[12px] text-base-content/60">
      每 60 秒检查一次注入状态，被其他工具改坏时自动恢复。
    </p>
  </SectionCard>

  <section class="rounded-box border border-error/40 bg-base-100">
    <header class="flex items-center justify-between border-b border-error/30 px-[18px] py-3">
      <h2 class="text-[15px] font-semibold">配置注入</h2>
      <span class="font-mono text-[10px] text-base-content/60">
        {{ codex?.injected ? '已注入' : '未注入' }}
      </span>
    </header>
    <div class="p-[18px]">
      <dl class="mb-4 grid gap-1.5 text-[12px]">
        <div class="flex gap-2">
          <dt class="w-20 shrink-0 text-base-content/50">配置文件</dt>
          <dd class="font-mono break-all">{{ codex?.configPath || '未找到' }}</dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-20 shrink-0 text-base-content/50">当前上游</dt>
          <dd class="font-mono break-all">{{ codex?.baseUrl || '—' }}</dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-20 shrink-0 text-base-content/50">备份</dt>
          <dd class="font-mono break-all">{{ codex?.hasBackup ? '已存在' : '尚未创建' }}</dd>
        </div>
      </dl>

      <div class="flex flex-wrap gap-2">
        <button class="btn btn-sm btn-outline active:scale-[.97]" :disabled="busy === 'config'" @click="askApply">
          注入配置
        </button>
        <button
          class="btn btn-sm btn-error btn-outline"
          :disabled="busy === 'config' || !codex?.hasBackup"
          @click="askRemove"
        >
          移除注入并还原
        </button>
      </div>
      <p v-if="!codex?.hasBackup" class="mt-2 text-[11px] text-base-content/50">
        尚无备份，因此「移除并还原」不可用 —— 没有可还原的原始状态。
      </p>
    </div>
  </section>

  <SectionCard title="转发 UA 兜底" meta="中转站兼容">
    <div class="flex flex-wrap items-center gap-3">
      <input
        v-model="fallbackUA"
        type="text"
        class="input input-sm w-full max-w-md"
        placeholder="留空 = 不补（转发请求不带 UA）"
      />
      <button class="btn btn-sm btn-outline active:scale-[.97]" :disabled="uaBusy" @click="saveUA">
        <span v-if="uaBusy" class="loading loading-spinner loading-xs" />
        保存
      </button>
    </div>
    <p class="mt-3 text-[11px] text-base-content/50">
      转发时除逐跳头外全部原样透传（含 User-Agent、Cookie、Authorization、各种 X-* 头），
      中转站看到的请求指纹与 codex 直连一致。仅当入站请求<b>没有</b> User-Agent 时才补这个兜底值；
      都为空时转发请求不带 UA —— 绝不泄漏 Go 默认 UA。
    </p>
  </SectionCard>

  <div v-if="showConfirm" class="modal modal-open" role="dialog">
    <div class="modal-box max-w-2xl">
      <h3 class="text-[15px] font-semibold">
        {{ pendingAction === 'apply' ? '确认注入配置？' : '确认移除注入？' }}
      </h3>

      <div v-if="pendingAction === 'apply' && preview.length" class="mt-3 space-y-2">
        <p class="text-[12px] text-base-content/70">将会改动以下内容：</p>
        <div
          v-for="(d, i) in preview"
          :key="i"
          class="rounded-field border border-base-300 bg-base-200/40 p-3 text-[12px]"
        >
          <div class="font-mono text-[11px] break-all text-base-content/60">{{ d.targetFile }}</div>
          <div class="mt-1 font-mono">{{ d.key }}</div>
          <div class="mt-1 text-base-content/70">
            <span class="text-base-content/50">原值</span> {{ d.oldValue || '(空)' }}
            <span class="mx-1.5">→</span>
            <span class="text-base-content/50">新值</span> {{ d.newValue }}
          </div>
          <div class="mt-1 text-[11px] text-base-content/50">撤销：{{ d.undoHint }}</div>
        </div>
      </div>

      <p v-else class="mt-3 text-[12px] text-base-content/70">
        将从备份还原 config.toml 到注入前的状态，并删除备份文件。
      </p>

      <div class="modal-action">
        <button class="btn btn-sm btn-ghost active:scale-[.97]" @click="showConfirm = false">取消</button>
        <button class="btn btn-sm btn-error btn-outline" @click="confirmDanger">
          {{ pendingAction === 'apply' ? '应用并备份' : '还原配置' }}
        </button>
      </div>
    </div>
  </div>
</template>

