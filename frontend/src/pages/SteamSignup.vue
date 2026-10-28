<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Icon from '@/components/Icon.vue'

type VerificationState = 'idle' | 'opened' | 'completed'

interface Draft {
  account: string
  email: string
  country: string
  password: string
  verification: VerificationState
  updatedAt: string
}

const STORAGE_KEY = 'helmx-steam-signup-draft'
const signupURL = 'https://store.steampowered.com/join/'

const account = ref('')
const email = ref('')
const country = ref('CN')
const password = ref('')
const verification = ref<VerificationState>('idle')
const notice = ref('')
const copied = ref('')

const accountOK = computed(() => /^[a-zA-Z0-9_]{3,32}$/.test(account.value))
const emailOK = computed(() => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value))
const passwordOK = computed(() => password.value.length >= 8 && password.value.length <= 64)
const formOK = computed(() => accountOK.value && emailOK.value && passwordOK.value && verification.value === 'completed')
const verificationLabel = computed(() => {
  if (verification.value === 'completed') return '已确认完成'
  if (verification.value === 'opened') return '等待确认'
  return '未开始'
})

function persist() {
  const draft: Draft = {
    account: account.value,
    email: email.value,
    country: country.value,
    password: password.value,
    verification: verification.value,
    updatedAt: new Date().toISOString(),
  }
  localStorage.setItem(STORAGE_KEY, JSON.stringify(draft))
}

function openSignup() {
  verification.value = 'opened'
  persist()
  notice.value = '已打开 Steam 官方注册页。完成人机验证后回到这里确认。'
  void import('@wailsio/runtime')
    .then(({ Browser }) => Browser.OpenURL(signupURL))
    .catch(() => window.open(signupURL, '_blank', 'noopener,noreferrer'))
}

function confirmVerification() {
  verification.value = 'completed'
  persist()
  notice.value = '人机验证状态已记录在本机。'
}

function resetVerification() {
  verification.value = 'idle'
  persist()
  notice.value = ''
}

function generatePassword() {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%'
  const bytes = crypto.getRandomValues(new Uint32Array(18))
  password.value = Array.from(bytes, (n) => chars[n % chars.length]).join('')
  persist()
  notice.value = '已生成随机密码，仅保存在本机草稿中。'
}

async function copyValue(label: string, value: string) {
  if (!value) return
  try {
    await navigator.clipboard.writeText(value)
    copied.value = label
    window.setTimeout(() => (copied.value = ''), 1400)
  } catch {
    notice.value = '当前环境不允许访问剪贴板，请手动复制。'
  }
}

function saveDraft() {
  persist()
  notice.value = '草稿已保存到本机浏览器存储。'
}

function clearDraft() {
  account.value = ''
  email.value = ''
  country.value = 'CN'
  password.value = ''
  verification.value = 'idle'
  notice.value = ''
  localStorage.removeItem(STORAGE_KEY)
}

onMounted(() => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return
    const draft = JSON.parse(raw) as Partial<Draft>
    account.value = draft.account ?? ''
    email.value = draft.email ?? ''
    country.value = draft.country ?? 'CN'
    password.value = draft.password ?? ''
    verification.value = draft.verification ?? 'idle'
  } catch {
    localStorage.removeItem(STORAGE_KEY)
  }
})
</script>

<template>
  <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
    <section class="rounded-box border border-base-300 bg-base-100">
      <header class="flex items-start justify-between gap-4 border-b border-base-300 px-[18px] py-4">
        <div>
          <h2 class="text-[15px] font-semibold">注册资料</h2>
          <p class="mt-1 text-[12px] text-base-content/60">资料只写入本机草稿，不会通过此应用提交。</p>
        </div>
        <span class="badge badge-sm" :class="formOK ? 'badge-success' : 'badge-ghost'">
          {{ formOK ? '可以继续' : '待完成' }}
        </span>
      </header>

      <div class="grid gap-4 p-[18px] sm:grid-cols-2">
        <label class="form-control sm:col-span-2">
          <span class="label-text text-[12px]">Steam 用户名</span>
          <input v-model.trim="account" class="input input-sm mt-1" placeholder="3–32 位字母、数字或下划线" @input="persist" />
          <span class="mt-1 text-[11px]" :class="accountOK ? 'text-success' : 'text-base-content/50'">{{ accountOK ? '格式有效' : '注册页会再次检查可用性' }}</span>
        </label>

        <label class="form-control">
          <span class="label-text text-[12px]">电子邮箱</span>
          <input v-model.trim="email" type="email" class="input input-sm mt-1" placeholder="name@example.com" @input="persist" />
          <span class="mt-1 text-[11px]" :class="emailOK ? 'text-success' : 'text-base-content/50'">{{ emailOK ? '格式有效' : '请输入可收信邮箱' }}</span>
        </label>

        <label class="form-control">
          <span class="label-text text-[12px]">国家/地区</span>
          <select v-model="country" class="select select-sm mt-1" @change="persist">
            <option value="CN">中国</option>
            <option value="HK">中国香港</option>
            <option value="TW">中国台湾</option>
            <option value="US">美国</option>
            <option value="JP">日本</option>
          </select>
        </label>

        <label class="form-control sm:col-span-2">
          <span class="label-text text-[12px]">密码</span>
          <div class="join mt-1 w-full">
            <input v-model="password" type="text" class="input input-sm join-item w-full" placeholder="至少 8 位" @input="persist" />
            <button class="btn btn-sm btn-outline join-item" type="button" title="生成随机密码" @click="generatePassword">生成</button>
            <button class="btn btn-sm btn-ghost join-item" type="button" title="复制密码" :disabled="!password" @click="copyValue('password', password)">{{ copied === 'password' ? '已复制' : '复制' }}</button>
          </div>
          <span class="mt-1 text-[11px]" :class="passwordOK ? 'text-success' : 'text-base-content/50'">{{ passwordOK ? '长度有效' : '建议使用 8–64 位随机密码' }}</span>
        </label>
      </div>

      <div class="mx-[18px] mb-[18px] rounded-field border border-base-300 bg-base-200/30 p-3">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div class="flex items-center gap-2 text-[13px] font-medium">
              <span class="status-dot size-1.5 rounded-full" :class="verification === 'completed' ? 'is-live' : 'bg-base-content/30'" />
              人机验证
            </div>
            <p class="mt-1 text-[11px] text-base-content/60">状态：{{ verificationLabel }}。验证在 Steam 官方页面完成。</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <button class="btn btn-sm btn-primary" type="button" @click="openSignup"><Icon name="services" :size="14" />打开官方注册页</button>
            <button v-if="verification === 'opened'" class="btn btn-sm btn-outline" type="button" @click="confirmVerification">我已完成验证</button>
            <button v-else-if="verification === 'completed'" class="btn btn-sm btn-ghost" type="button" @click="resetVerification">重置</button>
          </div>
        </div>
      </div>

      <div class="flex flex-wrap justify-end gap-2 border-t border-base-300 px-[18px] py-3">
        <button class="btn btn-sm btn-ghost" type="button" @click="clearDraft">清空本地草稿</button>
        <button class="btn btn-sm btn-outline" type="button" @click="saveDraft">保存草稿</button>
      </div>
    </section>

    <aside class="space-y-4">
      <section class="rounded-box border border-base-300 bg-base-100 p-4">
        <h2 class="text-[15px] font-semibold">本地检查</h2>
        <ul class="mt-3 space-y-2 text-[12px]">
          <li class="flex justify-between gap-3"><span>用户名格式</span><span :class="accountOK ? 'text-success' : 'text-base-content/50'">{{ accountOK ? '通过' : '待填' }}</span></li>
          <li class="flex justify-between gap-3"><span>邮箱格式</span><span :class="emailOK ? 'text-success' : 'text-base-content/50'">{{ emailOK ? '通过' : '待填' }}</span></li>
          <li class="flex justify-between gap-3"><span>密码长度</span><span :class="passwordOK ? 'text-success' : 'text-base-content/50'">{{ passwordOK ? '通过' : '待填' }}</span></li>
          <li class="flex justify-between gap-3"><span>人机验证</span><span :class="verification === 'completed' ? 'text-success' : 'text-base-content/50'">{{ verification === 'completed' ? '已确认' : '待确认' }}</span></li>
        </ul>
      </section>

      <section class="rounded-box border border-base-300 bg-base-100 p-4">
        <h2 class="text-[15px] font-semibold">流程</h2>
        <ol class="mt-3 space-y-2 text-[12px] text-base-content/70">
          <li><span class="mr-2 font-mono text-base-content/40">01</span>填写并保存本地资料</li>
          <li><span class="mr-2 font-mono text-base-content/40">02</span>打开 Steam 官方注册页</li>
          <li><span class="mr-2 font-mono text-base-content/40">03</span>在官方页面完成人机验证</li>
          <li><span class="mr-2 font-mono text-base-content/40">04</span>回到此页确认状态，再继续官方注册</li>
        </ol>
      </section>
    </aside>
  </div>

  <div v-if="notice" class="alert alert-info mt-4 py-2 text-[12px]">
    <span>{{ notice }}</span>
  </div>
</template>
