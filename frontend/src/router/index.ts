import { createRouter, createWebHashHistory } from 'vue-router'
import { ROUTES } from './nav'

// hash 模式：Wails 用 file 协议加载产物，history 模式会 404。
export const router = createRouter({
  history: createWebHashHistory(),
  routes: ROUTES.map((r) => ({ path: r.path, component: r.component })),
})
