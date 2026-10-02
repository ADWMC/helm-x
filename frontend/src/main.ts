import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { registerGlobalComponents } from './components'
import { detectBackend } from './api/backend'
import './style.css'

// 先探测后端是否可用，再挂载。
//
// 【为何放在这里】界面在"接后端"和"纯静态预览"两种情况下都要能用：
//   - 桌面窗口：Wails AssetServer 应答 /wails/runtime，走真实数据
//   - 浏览器预览：无应答，退化为演示数据并显示横幅
//
// 探测必须早于首次渲染。若放到各页 onMounted 里，九个页面都要处理时序，
// 而且会因为渲染发生在探测之前而闪一下空状态。
//
// 探测失败不会阻塞：最坏情况是多等一次 fetch 的超时，
// 此时按预览模式渲染，功能降级但界面可用。
async function bootstrap() {
  try {
    await detectBackend()
  } catch {
    // 探测本身不应成为启动失败的原因
  }

  const app = createApp(App)
  app.use(createPinia()).use(router)
  registerGlobalComponents(app)
  app.mount('#app')
}

void bootstrap()
