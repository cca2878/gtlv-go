// Command eval-model 用某模型跑 N 次线上点选，统计单次过码率 + 失败构成（检测 vs 匹配）
// + 失败时的最低匹配置信度分布。用于对比不同 siamese 模型、并为改进方向提供数据参考。
//
// 用法：go run ./cmd/eval-model -n 150（模型随 wasm 内嵌；换模型＝改 gtlv-core/modeldata 后重编）
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cca2878/gtlv-go/pkg/client"
	"github.com/cca2878/gtlv-go/pkg/solver"
)

func has(s, sub string) bool { return strings.Contains(s, sub) }

func main() {
	var (
		cacheDir = flag.String("cachedir", "/tmp/gtlv-cache", "wazero 缓存目录")
		n        = flag.Int("n", 150, "目标有效尝试数")
		regURL   = flag.String("register", client.BilibiliRegisterURL, "登记端点")
		saveDir  = flag.String("savedir", "", "存 verify 失败图目录（按 mk/mgtk 分类，含图+框 JSON），供针对性训练")
	)
	flag.Parse()

	sol, err := solver.NewCaptchaSolver(solver.WithCacheDir(*cacheDir))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = sol.Close() }()
	c := client.NewV3Client()

	cat := map[string]int{}
	var passConf, failConf []float64
	pass, total, saved := 0, 0, 0
	for total < *n {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		gt, challenge, err := client.Register(ctx, nil, *regURL)
		if err != nil {
			cancel()
			continue
		}
		mr, err := c.MineOnce(ctx, gt, challenge, sol)
		cancel()
		if err != nil {
			continue
		}
		total++
		if mr.Passed {
			pass++
			if mr.Result != nil {
				passConf = append(passConf, minConf(mr.Result))
			}
			continue
		}
		// 失败归类（sub=存图子目录），所有失败都可存图
		var sub string
		m, k := 0, 0
		switch {
		case mr.SolveErr != "":
			switch {
			case has(mr.SolveErr, "tiles <"):
				sub = "detect_miss_mLTk"
			case has(mr.SolveErr, "got k="):
				sub = "k_range_aspect"
			case has(mr.SolveErr, "prompt"):
				sub = "no_prompt"
			case has(mr.SolveErr, "answer"):
				sub = "no_answer"
			case has(mr.SolveErr, "no_matches"):
				sub = "no_matches"
			default:
				sub = "other_solve"
			}
		case mr.Result != nil:
			for _, d := range mr.Result.Detections {
				if d.ClassID == 0 {
					m++
				}
			}
			k = len(mr.Result.Matches)
			failConf = append(failConf, minConf(mr.Result))
			if m > k {
				sub = "verify_mgtk_match"
			} else {
				sub = "verify_mk_missOrPerm"
			}
		default:
			sub = "unknown"
		}
		cat[sub]++
		if *saveDir != "" {
			dir := filepath.Join(*saveDir, sub)
			_ = os.MkdirAll(dir, 0o755)
			base := filepath.Join(dir, fmt.Sprintf("%04d_m%dk%d", saved, m, k))
			_ = os.WriteFile(base+".jpg", mr.Image, 0o644)
			meta := map[string]any{"gt": gt, "challenge": challenge, "m": m, "k": k, "solve_err": mr.SolveErr, "result": mr.Result}
			if b, e := json.MarshalIndent(meta, "", " "); e == nil {
				_ = os.WriteFile(base+".json", b, 0o644)
			}
			saved++
		}
		if total%25 == 0 {
			fmt.Fprintf(os.Stderr, "[%d/%d] pass=%d\n", total, *n, pass)
		}
	}

	fmt.Printf("\n===== 模型: (随 wasm 内嵌) =====\n")
	fmt.Printf("单次过码: %d/%d = %.1f%%\n", pass, total, 100*float64(pass)/float64(total))
	fmt.Println("失败构成:")
	type kv struct {
		k string
		v int
	}
	var rows []kv
	for k, v := range cat {
		rows = append(rows, kv{k, v})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].v > rows[j].v })
	fails := total - pass
	for _, r := range rows {
		fmt.Printf("  %-28s %3d  (%.0f%% of fails)\n", r.k, r.v, 100*float64(r.v)/float64(max1(fails)))
	}
	fmt.Printf("最低匹配置信度  过码: %s\n", stat(passConf))
	fmt.Printf("               verify失败: %s\n", stat(failConf))
}

func minConf(r *solver.Result) float64 {
	m := 1.0
	for _, x := range r.Matches {
		if x.Confidence < m {
			m = x.Confidence
		}
	}
	return m
}

func stat(v []float64) string {
	if len(v) == 0 {
		return "(无)"
	}
	sort.Float64s(v)
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return fmt.Sprintf("n=%d mean=%.3f median=%.3f min=%.3f", len(v), sum/float64(len(v)), v[len(v)/2], v[0])
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
