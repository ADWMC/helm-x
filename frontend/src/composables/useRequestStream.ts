// useRequestStream —— 控制台的实时请求流（SSE）。
//
// 为什么不走 Wails 自定义事件：`proxy:request` 在本机 WebView2 下一条都到不了
// （2026-11-22 实测，自检页因此暴露）。代理本身是 HTTP 服务，在 loopback 上开
// 一条只读 SSE（`/__helmx/events`）更可控，也不依赖框架事件桥。
//
// 用法：
//   const { start, stop } = useRequestStream((rec) => { ... })
//   start(listenAddr)   // 形如 "127.0.0.1:1800"
//
// 失败一律静默（调用方保留自己的轮询兜底）：EventSource 报错就 stop，不重试轰炸。
import type { RequestRecord } from '@/api/backend'

export function useRequestStream(onRecord: (rec: RequestRecord) => void) {
  let es: EventSource | null = null

  function stop() {
    if (es) {
      es.close()
      es = null
    }
  }

  function start(listen: string) {
    stop()
    if (!listen) return
    try {
      es = new EventSource(`http://${listen}/__helmx/events`)
    } catch {
      es = null
      return
    }
    es.onmessage = (ev) => {
      try {
        const rec = JSON.parse(ev.data) as RequestRecord
        if (rec?.id) onRecord(rec)
      } catch {
        // 坏帧忽略：实时流是旁路，不能因为它打断页面
      }
    }
    es.onerror = () => {
      // 代理未启动/端口被占：关掉自己，页面继续用轮询
      stop()
    }
  }

  return { start, stop }
}
