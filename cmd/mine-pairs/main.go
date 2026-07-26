// Command mine-pairs 采集服务器验证过的度量学习训练对：反复取点选图 → 本地求解 → 提交，
// 每次 PASS 就从原图裁出 k 对跨域正样本(提示段↔点中tile) + 未点中的干扰(硬负样本)，
// 写成 groups.csv(同 group=正/异 group=负)。另记每次 PASS 的最低匹配置信度(passes.csv)，
// 供后续"低置信路由人工"分析。单图单提交，与线上一致。
//
// 用法：go run ./cmd/mine-pairs -out /path/pairs_dump -n 300（模型随 wasm 内嵌）
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cca2878/gtlv-go/pkg/client"
	"github.com/cca2878/gtlv-go/pkg/solver"
)

func cropSave(im image.Image, x0, y0, x1, y1 float64, path string) bool {
	r := image.Rect(int(x0), int(y0), int(x1), int(y1)).Intersect(im.Bounds())
	if r.Dx() < 4 || r.Dy() < 4 {
		return false
	}
	sub, ok := im.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return false
	}
	f, err := os.Create(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	return png.Encode(f, sub.SubImage(r)) == nil
}

func main() {
	var (
		cacheDir = flag.String("cachedir", "/tmp/gtlv-cache", "wazero 编译缓存目录")
		outDir   = flag.String("out", "pairs_dump", "输出目录")
		nPass    = flag.Int("n", 300, "目标 PASS 数")
		budget   = flag.Int("budget", 0, "最多尝试次数（0=自动 = n/0.6 * 1.5）")
		regURL   = flag.String("register", client.BilibiliRegisterURL, "登记端点")
	)
	flag.Parse()
	if *budget == 0 {
		*budget = *nPass * 3
	}
	cropDir := filepath.Join(*outDir, "crops")
	if err := os.MkdirAll(cropDir, 0o755); err != nil {
		fatal(err)
	}

	sol, err := solver.NewCaptchaSolver(solver.WithCacheDir(*cacheDir))
	if err != nil {
		fatal(err)
	}
	defer func() { _ = sol.Close() }()
	c := client.NewV3Client()

	var groups []string // "path,group"
	passes := []string{"pass,k,distractors,minconf"}
	pass, total := 0, 0
	for attempt := 0; attempt < *budget && pass < *nPass; attempt++ {
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
		if !mr.Passed || mr.Result == nil || mr.Result.PromptBox == nil {
			continue
		}
		im, err := jpeg.Decode(bytes.NewReader(mr.Image))
		if err != nil {
			continue
		}
		res := mr.Result
		pb := res.PromptBox
		k := len(res.Matches)
		segw := (pb.XMax - pb.XMin) / float64(k)
		var ans []solver.BoundingBox
		for _, d := range res.Detections {
			if d.ClassID == 0 {
				ans = append(ans, d.Box)
			}
		}
		minConf := 1.0
		matched := map[int]bool{}
		ok := true
		for _, m := range res.Matches {
			if m.AnswerIndex < 0 || m.AnswerIndex >= len(ans) {
				ok = false
				break
			}
			if m.Confidence < minConf {
				minConf = m.Confidence
			}
			matched[m.AnswerIndex] = true
			grp := fmt.Sprintf("%04d_%d", pass, m.PromptIndex)
			pP := filepath.Join(cropDir, fmt.Sprintf("%04d_p%d.png", pass, m.PromptIndex))
			tP := filepath.Join(cropDir, fmt.Sprintf("%04d_t%d.png", pass, m.PromptIndex))
			e1 := cropSave(im, pb.XMin+float64(m.PromptIndex)*segw, pb.YMin, pb.XMin+float64(m.PromptIndex+1)*segw, pb.YMax, pP)
			tb := ans[m.AnswerIndex]
			e2 := cropSave(im, tb.XMin, tb.YMin, tb.XMax, tb.YMax, tP)
			if !e1 || !e2 {
				ok = false
				break
			}
			groups = append(groups, pP+","+grp, tP+","+grp)
		}
		if !ok {
			continue
		}
		for j, ab := range ans {
			if matched[j] {
				continue
			}
			dP := filepath.Join(cropDir, fmt.Sprintf("%04d_d%d.png", pass, j))
			if cropSave(im, ab.XMin, ab.YMin, ab.XMax, ab.YMax, dP) {
				groups = append(groups, fmt.Sprintf("%s,%04d_d%d", dP, pass, j))
			}
		}
		passes = append(passes, fmt.Sprintf("%04d,%d,%d,%.3f", pass, k, len(ans)-k, minConf))
		pass++
		if pass%10 == 0 {
			fmt.Fprintf(os.Stderr, "[%d/%d] total=%d 正对=%d\n", pass, *nPass, total, len(groups))
		}
	}
	_ = os.WriteFile(filepath.Join(*outDir, "groups.csv"), []byte("path,group\n"+strings.Join(groups, "\n")+"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(*outDir, "passes.csv"), []byte(strings.Join(passes, "\n")+"\n"), 0o644)
	fmt.Fprintf(os.Stderr, "完成：%d PASS / %d 尝试 → %s (正对+负 crop 行=%d)\n", pass, total, *outDir, len(groups))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
