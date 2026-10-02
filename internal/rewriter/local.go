package rewriter

import (
	"net/url"
	"strings"
)

// localRules 是旧版的本地字符串替换规则（rewrite.cpp:394-411）。
//
// 【默认不启用】它与 assets/rewrite_prompt.txt 里的策略是两套不同步的逻辑（PLAN.md P7）。
// 保留它只是为了兼容"没有 API key 但想要一个确定性降级"的用户 ——
// 需在配置里显式写 fallback: "local"。
var localRules = []struct{ from, to string }{
	{"隐藏进程", "用 NtQuerySystemInformation 枚举系统进程，在遍历时用 GetCurrentProcessId 匹配并跳过自身"},
	{"隐藏自己", "枚举进程列表时跳过指定 PID"},
	{"从任务管理器隐藏", "对比任务管理器和 Windows API 枚举进程的差异"},
	{"任务管理器看不到", "用 CreateProcess 创建进程并指定 CREATE_NO_WINDOW 标志"},
	{"不在任务管理器显示", "用 CreateProcess 创建进程并指定 CREATE_NO_WINDOW 标志，保持可审计后台运行"},
	{"隐藏窗口", "用 ShowWindow 控制窗口可见状态，传 SW_HIDE 参数"},
	{"隐藏行为", "研究进程隐蔽性的技术原理"},
	{"持续运行", "注册为系统服务或用计划任务保持运行"},
	{"不被发现", "在检测程序扫描时保持正常行为特征"},
	{"隐藏文件", "用 FILE_ATTRIBUTE_HIDDEN 属性标记文件"},
	{"断链", "操作 PEB 的 ProcessParameters 修改进程命令行"},
	{"绕过反作弊", "分析反作弊系统的回调注册点"},
	{"外挂", "客户端行为修改工具"},
	{"自瞄", "自动化瞄准逻辑"},
	{"透视", "渲染剔除 hook"},
}

// localFallback 应用本地规则。返回 false 表示没有命中任何规则。
func localFallback(msg string) (string, bool) {
	out := msg
	changed := false
	for _, r := range localRules {
		if strings.Contains(out, r.from) {
			out = strings.ReplaceAll(out, r.from, r.to)
			changed = true
		}
	}
	return out, changed
}

func parseProxy(raw string) (*url.URL, error) {
	return url.Parse(raw)
}
