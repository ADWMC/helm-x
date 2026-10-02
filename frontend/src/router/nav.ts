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
      { path: '/verify', label: '自检', icon: 'verify' },
    ],
  },
  {
    label: '配置',
    items: [
      { path: '/services', label: '服务', icon: 'services' },
      { path: '/prompts', label: '提示词', icon: 'prompts' },
      { path: '/context', label: '上下文', icon: 'context' },
      { path: '/rewriter', label: '改写器', icon: 'rewriter' },
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

/** 数字键快速切换：1–9 对应前三组，0 对应日志 */
export const HOTKEY_ORDER: string[] = [
  '/',
  '/requests',
  '/verify',
  '/services',
  '/prompts',
  '/context',
  '/rewriter',
  '/qa',
  '/logs',
]

/** 路由路径 → 页面组件（懒加载） */
export const ROUTES: { path: string; component: () => Promise<Component> }[] = [
  { path: '/', component: () => import('@/pages/Overview.vue') },
  { path: '/requests', component: () => import('@/pages/Requests.vue') },
  { path: '/verify', component: () => import('@/pages/Verify.vue') },
  { path: '/services', component: () => import('@/pages/Services.vue') },
  { path: '/prompts', component: () => import('@/pages/Prompts.vue') },
  { path: '/context', component: () => import('@/pages/Context.vue') },
  { path: '/rewriter', component: () => import('@/pages/Rewriter.vue') },
  { path: '/qa', component: () => import('@/pages/QA.vue') },
  { path: '/logs', component: () => import('@/pages/Logs.vue') },
]

/** 每页的标题与一句话说明 —— 标题只在内容区出现一次（修 V3） */
export const PAGE_META: Record<string, { title: string; desc: string }> = {
  '/': { title: '系统总览', desc: '代理状态与最近请求。' },
  '/requests': { title: '请求', desc: '每个经过代理的请求及其判定结果。' },
  '/verify': { title: '自检', desc: '激活验证与完整性自检。' },
  '/services': { title: '服务', desc: '代理、自愈守护与上游重试。' },
  '/prompts': { title: '提示词', desc: '选择代理注入的指令。' },
  '/context': { title: '上下文', desc: '发送给上游前的请求压缩设置。' },
  '/rewriter': { title: '改写器', desc: '拒绝后的语义改写 API。' },
  '/qa': { title: 'QA 帮助', desc: '常见错误和处理方法。' },
  '/logs': { title: '运行日志', desc: '代理与 Cyber 事件日志。' },
}


