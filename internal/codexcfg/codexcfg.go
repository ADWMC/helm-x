// Package codexcfg 负责读写用户的 codex 配置：~/.codex/config.toml。
//
// 职责边界（决定：docs/adr/ADR-001-toml-handling.md）：
//   - 本包**不是**通用 TOML 库，只做「对用户文件的最小、可逆点修改」
//   - 只识别结构（表头 / 键行 / 注释 / 空行），**不解析值语义**
//   - 值一律作为原始字节处理，未改动的行保证逐字节不变（INV-6）
//
// 文件职责：
//
//	locate.go  定位 codex home 与配置文件
//	doc.go     行级文档模型：解析、查表栈、取值
//	edit.go    点修改：设置键、插入键、替换 provider 的 base_url
//	backup.go  备份链与还原：原子写、还原点、逐字节回滚
//	probe.go   只读探测：激活 provider、relay URL、注入状态
//
// 明确不做：多行字符串、[[array of tables]] —— 检测到即拒绝写入并报错，
// 而不是猜测（见 ADR-001「接受的风险」）。
package codexcfg
