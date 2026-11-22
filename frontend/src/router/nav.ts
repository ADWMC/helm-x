import type { Component } from 'vue'

import type { IconName } from '@/components/Icon.vue'

export interface NavItem {
  path: string
  label: string
  /**
   * 图标名，对应 Icon.vue 的 PATHS。
   *
   * 【为何改】原设计用文本字符（▦ ⇄ ✓ ⏻ ✎ ≡ ⟳ ⌗ ? ▤）。
   * 问题：这些字符来自不同 Unicode 区段，字重与视觉大小无法统一，
   * 且部分字体缺字会退化成豆腐块。改为统一描边的矢量图标。
   */
  icon: IconName
}

export interface NavGroup {
  label: string
  items: NavItem[]
}

/**
 * 导航分组。沿用原 dashboard 的 概览 / 配置 / 调试 / 日志。
 *
 * 【已删除「规则」页】该页只列出编译进二进制的 TAMPER 正则，
 * 唯一交互是一个搜索框 —— 不能编辑、不能保存、不能开关，
 * 改了要重新构建。它把「实现细节」当成「用户可操作项」摆在导航里，
 * 属于界面噪音，已连同后端 RulesService 一起移除。
 */
export const NAV_GROUPS: NavGroup[] = [
  {
    label: '概览',
    items: [
      { path: '/', label: '系统总览', icon: 'dashboard' },
      { path: '/requests', label: '请求', icon: 'requests' },
      { path: '/capture', label: '报文', icon: 'capture' },
      { path: '/verify', label: '自检', icon: 'verify' },
    ],
  },
  {
    label: '配置',
    items: [
      { path: '/services', label: '服务', icon: 'services' },
      { path: '/prompts', label: '提示词', icon: 'prompts' },
      { path: '/armor', label: '破甲', icon: 'armor' },
    ],
  },
  {
    label: '调试',
    items: [{ path: '/qa', label: 'QA 帮助', icon: 'qa' }],
  },
  {
    label: '日志',
    items: [{ path: '/logs', label: '运行日志', icon: 'logs' }],
  },
]

/** 数字键快速切换：1–9 依次对应下列页面，0 固定回「运行日志」 */
export const HOTKEY_ORDER: string[] = [
  '/',
  '/requests',
  '/capture',
  '/verify',
  '/services',
  '/prompts',
  '/armor',
  '/qa',
]

/** 路由路径 → 页面组件（懒加载） */
export const ROUTES: { path: string; component: () => Promise<Component> }[] = [
  { path: '/', component: () => import('@/pages/Overview.vue') },
  { path: '/requests', component: () => import('@/pages/Requests.vue') },
  { path: '/capture', component: () => import('@/pages/Capture.vue') },
  { path: '/verify', component: () => import('@/pages/Verify.vue') },
  { path: '/services', component: () => import('@/pages/Services.vue') },
  { path: '/prompts', component: () => import('@/pages/Prompts.vue') },
  { path: '/armor', component: () => import('@/pages/Armor.vue') },
  { path: '/qa', component: () => import('@/pages/QA.vue') },
  { path: '/logs', component: () => import('@/pages/Logs.vue') },
]

/**
 * 每页的标题。标题在内容区只出现一次（修 V3）。
 *
 * 【2026-11-22 删掉 desc】原来每页标题下面还有一句"这页干什么"的说明，
 * 属于写给自己/评审的解释，使用者点进来就知道这页是干什么的 ——
 * 说明文字只在会话里讲，UI 里不留。
 */
export const PAGE_META: Record<string, { title: string }> = {
  '/': { title: '系统总览' },
  '/requests': { title: '请求' },
  '/capture': { title: '报文' },
  '/verify': { title: '自检' },
  '/services': { title: '服务' },
  '/prompts': { title: '提示词' },
  '/armor': { title: '破甲' },
  '/qa': { title: 'QA 帮助' },
  '/logs': { title: '运行日志' },
}
