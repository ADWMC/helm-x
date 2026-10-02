/**
 * 生成的 Wails 绑定在两种模式下形态不同：
 *
 *   wails3 dev            → bindings/**.js  （无类型声明）
 *   wails3 build/package  → bindings/**.ts  （有类型，且本文件不应介入）
 *
 * 本文件只在**没有真实类型**时兜底。TypeScript 的模块声明与真实
 * .ts 定义同时存在时以真实定义为准，因此生产模式不受影响。
 *
 * 注意：不能只写 `declare module '...'` 匹配整个目录 ——
 * 那会连带覆盖生产模式的类型。这里逐个声明服务模块。
 */

declare module '*/bindings/github.com/ADWMC/helm-x/internal/svc' {
  const mod: {
    ProxyService: ServiceModule
    ConfigService: ServiceModule
    PromptService: ServiceModule
    RewriterService: ServiceModule
    VerifyService: ServiceModule
    LogService: ServiceModule
    QAService: ServiceModule
  }
  export = mod
}

/** 绑定服务：方法名 → 调用。具体签名由 Go 侧方法决定。 */
interface ServiceModule {
  [method: string]: (...args: never[]) => Promise<unknown>
}

