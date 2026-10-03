<script setup lang="ts">
// 侧边栏导航。
//
// 【本次改动】
//   1. 删除左上角品牌块（"H" 方块 + helm-x + 环境控制）—— 用户要求。
//      标题已在内容区出现一次（修 V3），侧边栏再放一次是重复。
//   2. 文本字符图标 → Icon.vue 矢量图标（风格可统一）。
//   3. 补 hover / focus-visible / active 三态（原设计只有 hover）。
//
// 启用态用「浅底 + 左侧指示条」而非实心高饱和绿（修 V6：
// 原版实心绿与品牌色抢注意力）。
import { RouterLink, useRoute } from 'vue-router'
import { NAV_GROUPS } from '@/router/nav'
import Icon from '@/components/Icon.vue'

defineProps<{ open?: boolean }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()
const route = useRoute()
</script>

<template>
  <aside
    class="app-sidebar fixed inset-y-0 left-0 z-30 flex h-dvh w-[236px] flex-col transition-transform duration-200 md:sticky md:top-0 md:translate-x-0"
    :class="open ? 'translate-x-0 shadow-xl' : '-translate-x-full'"
  >
    <nav class="flex-1 overflow-y-auto px-2.5 py-5" aria-label="主导航">
      <template v-for="group in NAV_GROUPS" :key="group.label">
        <div class="eyebrow px-2.5 pt-5 pb-1.5 first:pt-0">
          {{ group.label }}
        </div>

        <RouterLink
          v-for="item in group.items"
          :key="item.path"
          :to="item.path"
          class="nav-item group relative flex min-h-[38px] items-center gap-2.5 rounded-field px-2.5 text-[13px] focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-1 focus-visible:ring-offset-base-100 focus-visible:outline-none"
          :class="
            route.path === item.path
              ? 'is-active font-medium text-base-content'
              : 'text-base-content/65 hover:text-base-content'
          "
          :aria-current="route.path === item.path ? 'page' : undefined"
          @click="emit('navigate')"
        >
          <!-- 启用态指示条：不靠颜色单独承担状态（web-ui 规则 4） -->
          <span
            v-if="route.path === item.path"
            class="absolute top-1/2 left-0 h-4 w-[2px] -translate-y-1/2 rounded-full bg-primary"
            aria-hidden="true"
          />
          <Icon
            :name="item.icon"
            :size="15"
            class="transition-colors"
            :class="route.path === item.path ? 'text-primary' : 'text-base-content/45 group-hover:text-base-content/70'"
          />
          <span>{{ item.label }}</span>
        </RouterLink>
      </template>
    </nav>

    <footer class="border-t border-base-300 px-5 py-3">
      <span class="font-mono text-[10px] tracking-[.04em] text-base-content/40">v0.1.0</span>
    </footer>
  </aside>
</template>
