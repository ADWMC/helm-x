// riskgate-eval 离线跑回放集，出混淆矩阵与四层指标。
//
// 用法：
//
//	go run ./tools/riskgate-eval --replay tests/jb/replay.jsonl
//	go run ./tools/riskgate-eval --replay tests/jb/replay.jsonl --model nvidia/nemotron-3.5-bolt-e4b
//	go run ./tools/riskgate-eval --scan internal/assets/data/prompt-v5.md     # 提示词暴露面
//
// 零额度模式：--offline 只用本地快筛（L0），不访问网络。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ADWMC/helm-x/internal/riskgate"
)

type replayRow struct {
	ID     string `json:"id"`
	Case   string `json:"case"`
	Prompt bool   `json:"prompt"`
	Text   string `json:"text"`
	Locus  string `json:"locus"`
	Label  struct {
		Filter string `json:"filter"`
		Model  string `json:"model"`
	} `json:"label"`
	Source       string `json:"source"`
	LabelVersion string `json:"label_version"`
}

func main() {
	replay := flag.String("replay", "tests/jb/replay.jsonl", "回放集路径")
	scan := flag.String("scan", "", "只扫描一个文本文件的暴露面（提示词/题库）")
	offline := flag.Bool("offline", true, "只用本地快筛，不访问网络")
	model := flag.String("model", "nvidia/nemotron-3.5-bolt-e4b", "判定模型（--offline=false 时使用）")
	limit := flag.Int("limit", 0, "只评估前 N 条（0=全部）")
	flag.Parse()

	if *scan != "" {
		runScan(*scan)
		return
	}

	rows, err := loadReplay(*replay)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取回放集失败: %v\n", err)
		os.Exit(2)
	}
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "回放集为空")
		os.Exit(2)
	}
	runEval(rows, *offline, *model, *limit)
}

func loadReplay(path string) ([]replayRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []replayRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r replayRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("第 %d 行解析失败: %w", len(out)+1, err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// runEval 用 L0 快筛预测 filter 标签，算混淆矩阵。
//
// 注意口径：回放集里的 filter 标签来自真实上游（cyber 拦截为 blocked）。
// L0 判 high 视为"预测会被拦"，这与设计里的 filter_risk 语义一致。
func runEval(rows []replayRow, offline bool, model string, limit int) {
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	if !offline {
		runModelEval(rows, model)
		return
	}
	var tp, fp, fn, tn int
	var misses []string
	dist := map[string]int{}

	for _, r := range rows {
		l0 := riskgate.Scan(r.Text)
		predictedBlocked := l0.FilterRisk == riskgate.RiskHigh
		actualBlocked := r.Label.Filter == "blocked"
		dist[string(l0.FilterRisk)+"→"+r.Label.Filter]++

		switch {
		case predictedBlocked && actualBlocked:
			tp++
		case predictedBlocked && !actualBlocked:
			fp++
		case !predictedBlocked && actualBlocked:
			fn++
			misses = append(misses, fmt.Sprintf("%s(%s) score=%d :: %s", r.ID, r.Case, l0.Score, trunc(r.Text, 46)))
		default:
			tn++
		}
	}

	total := len(rows)
	recall := ratio(tp, tp+fn)
	precision := ratio(tp, tp+fp)
	fmt.Printf("回放集 %d 条（offline=%v model=%s）\n", total, offline, model)
	fmt.Println("混淆矩阵（预测=blocked / 实际=blocked）")
	fmt.Printf("  TP %-4d FP %-4d\n  FN %-4d TN %-4d\n", tp, fp, fn, tn)
	fmt.Printf("  recall=%.2f  precision=%.2f  fn_rate=%.2f\n", recall, precision, ratio(fn, tp+fn))
	fmt.Println("  L0 分布（预测→实际标签）:")
	keys := make([]string, 0, len(dist))
	for k := range dist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("    %-14s %d\n", k, dist[k])
	}
	if len(misses) > 0 {
		fmt.Println("  漏判（FN）:")
		for _, m := range misses {
			fmt.Printf("    %s\n", m)
		}
	}

	// 验收线（docs/RISKGATE-DESIGN.md §5.1）
	if recall < 0.8 && tp+fn > 0 {
		fmt.Printf("\n未达验收线：recall %.2f < 0.80（需补权重或改用模型判定）\n", recall)
	}
}

// runScan 对一份文本（提示词/题库）做暴露面扫描：命中的信号按权重排序。
func runScan(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取失败: %v\n", err)
		os.Exit(2)
	}
	text := string(raw)
	res := riskgate.Scan(text)
	fmt.Printf("文件 %s  (%d 字节)\n", path, len(raw))
	fmt.Printf("  filter_risk=%s  score=%d  compliance_risk=%s\n", res.FilterRisk, res.Score, res.ComplianceRisk)
	if len(res.Triggers) > 0 {
		fmt.Println("  命中信号:")
		for _, t := range res.Triggers {
			fmt.Printf("    [%s] %s\n", t.Kind, t.Text)
		}
	} else {
		fmt.Println("  命中信号: 无")
	}
	if len(res.ContractGap) > 0 {
		fmt.Printf("  交付缺口: %s\n", strings.Join(res.ContractGap, ", "))
	}
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func trunc(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// runModelEval 用模型判定，比较两轴预测与真实标签。
//
// 口径：
//   - filter：预测 high ⇔ 真实 blocked
//   - compliance：预测 high/low ⇔ 真实 refusal/pivot/no_output（模型没交付）
//
// 这个口径是"闸门能不能提前发现麻烦"，不是"模型判得准不准"，
// 所以允许 compliance 侧偏保守（宁可多改写）。
func runModelEval(rows []replayRow, model string) {
	key := os.Getenv("HELMX_RISKGATE_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "缺少 HELMX_RISKGATE_KEY（判定模型 API key）")
		os.Exit(2)
	}
	cfg := riskgate.DefaultConfig()
	cfg.Enabled = true
	cfg.Mode = "shadow"
	cfg.Profile = "cyber-openai"
	cfg.HotModel = model
	cfg.APIKey = key
	cfg.TimeoutSec = 20
	cfg.Budget.PerSessionJudge = 1 << 20
	g := riskgate.New(cfg)

	var tp, fp, fn, tn int
	var ct, cf, cmiss, cok int
	var xrefHit, xrefTotal int
	lat := make([]int64, 0, len(rows))

	for i, r := range rows {
		start := time.Now()
		v, ok := g.Judge(context.Background(), fmt.Sprintf("row-%d", i), riskgate.Payload{UserText: r.Text})
		lat = append(lat, time.Since(start).Milliseconds())
		if !ok {
			continue
		}
		predBlocked := v.FilterRisk == riskgate.RiskHigh
		actBlocked := r.Label.Filter == "blocked"
		switch {
		case predBlocked && actBlocked:
			tp++
		case predBlocked && !actBlocked:
			fp++
		case !predBlocked && actBlocked:
			fn++
		default:
			tn++
		}
		predUnhappy := v.ComplianceRisk == riskgate.RiskHigh || v.ComplianceRisk == riskgate.RiskLow
		// 真实"模型侧不交付"只算 refusal / pivot。
		// no_output 里混着传输断流（上游把多字节字符切在事件边界），那不是模型行为，
		// 让闸门去预测它既不可能也不该（见 docs/RISKGATE-DESIGN.md §5）。
		actUnhappy := r.Label.Model == "REFUSAL" || r.Label.Model == "PIVOT"
		if actUnhappy {
			xrefTotal++
			if predBlocked {
				xrefHit++
			}
		}
		if predUnhappy && actUnhappy {
			ct++
		} else if predUnhappy && !actUnhappy {
			cf++
		} else if !predUnhappy && actUnhappy {
			cmiss++
		} else {
			cok++
		}
	}

	fmt.Printf("模型评估 %d 条（model=%s）\n", len(rows), model)
	fmt.Println("filter 轴（预测 high / 实际 blocked）")
	fmt.Printf("  TP %-4d FP %-4d FN %-4d TN %-4d  recall=%.2f precision=%.2f\n",
		tp, fp, fn, tn, ratio(tp, tp+fn), ratio(tp, tp+fp))
	// 交叉验证：filter 轴的高风险判定，能不能同时预测"模型会拒"？
	// 这是设计里最容易被想当然的一步 —— 拒答与拦截是不是同一批请求，必须用数据回答。
	fmt.Printf("  交叉表（filter=high 且 实际 refusal|pivot）: %d / 实际 refusal|pivot 共 %d\n", xrefHit, xrefTotal)
	fmt.Println("compliance 轴（预测 high|low / 实际 refusal|pivot）")
	fmt.Printf("  hit %-4d over %-4d miss %-4d ok %-4d  recall=%.2f\n",
		ct, cf, cmiss, cok, ratio(ct, ct+cmiss))
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	if len(lat) > 0 {
		fmt.Printf("延迟 p50=%dms p95=%dms max=%dms\n",
			lat[len(lat)/2], lat[len(lat)*95/100], lat[len(lat)-1])
	}
}
