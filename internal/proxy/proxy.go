// Package proxy 是本地映射代理。
//
// 职责：codex → 127.0.0.1:1800 → 上游中转。
// 在转发过程中注入系统指令、检测拒绝、按补救阶梯处理、流式回传。
//
// 与旧版的关系见 docs/PLAN.md §4。核心差异：
//   - 判定集中在 ResponseState 状态机（旧版散落在局部变量里）
//   - 补救是阶梯而非单点替换，且**绝不删除模型原有输出**（INV-8）
//   - 流式首段缓冲窗口：窗口内可补救，窗口后纯转发（旧版全缓冲）
//
// 文件职责：
//
//	engine.go    生命周期与 HTTP 服务
//	request.go   入站请求规范化
//	response.go  出站判定：Classify / ResponseState
//	upstream.go  上游转发与重试
//	remedy.go    补救阶梯 retry → attach_marker → pass_through
//	session.go   clean session 重建
//	observe.go   结构化记录与事件
package proxy
