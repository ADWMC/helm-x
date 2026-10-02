import type { App } from 'vue'
import SectionCard from './SectionCard.vue'
import StatTile from './StatTile.vue'
import VerdictBadge from './VerdictBadge.vue'
import LogStream from './LogStream.vue'
import SkeletonRows from './SkeletonRows.vue'
import Icon from './Icon.vue'

// 基础件全局注册，避免每个页面重复 import。
// 只放跨页面复用的无业务语义组件；有业务语义的按需局部引入。
export function registerGlobalComponents(app: App) {
  app.component('SectionCard', SectionCard)
  app.component('StatTile', StatTile)
  app.component('VerdictBadge', VerdictBadge)
  app.component('LogStream', LogStream)
  app.component('SkeletonRows', SkeletonRows)
  app.component('Icon', Icon)
}
