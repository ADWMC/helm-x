// Package svc 是 Wails 绑定层：前端唯一入口。
//
// 职责边界（关键）：
//   - **只有本包与 main 的 GUI 入口依赖 Wails**。其余 internal 包零 Wails 依赖，
//     这样 Wails 升级 beta 版本时改动面被限制在一处（RISKS.md R-4）。
//   - 本包只做参数校验、调用领域包、把结果转成前端友好的结构。
//     **不做业务判断** —— 判定在 proxy、注入在 codexcfg、自检在 selfcheck。
//
// 文件职责：
//
//	services.go  服务容器与公共类型
//	proxy.go     代理启停与状态
//	config.go    codex 配置状态 / apply / remove / 还原
//	prompt.go    提示词模式
//	rewriter.go  改写器读写与测试
//	verify.go    自检（异步 + 进度事件）
//	logs.go      日志与请求记录
//	qa.go        QA 数据
//	app.go       窗口、托盘、生命周期
package svc
