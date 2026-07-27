// gt-captcha-test 是一个本地测试 CLI 工具，用于开发调试。
//
// 使用示例：
//
//	gt-captcha-test -image testdata/sample.jpg -gt xxx -challenge yyy
//	gt-captcha-test -image testdata/sample.jpg -gt xxx -challenge yyy -verbose
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cca2878/gtlv-go/pkg/crypto"
	"github.com/cca2878/gtlv-go/pkg/solver"
)

func main() {
	imagePath := flag.String("image", "", "验证码图片路径 (JPEG/PNG)")
	gt := flag.String("gt", "", "验证码 gt 参数")
	challenge := flag.String("challenge", "", "验证码 challenge 参数")
	wasmPath := flag.String("wasm", "", "外部 .wasm 模块路径（可选；默认用内嵌模块）")
	verbose := flag.Bool("verbose", false, "输出详细信息")
	enablePerf := flag.Bool("perf", true, "启用性能计时")
	confThreshold := flag.Float64("conf", float64(solver.DefaultConfThreshold), "置信度阈值")
	cacheDir := flag.String("cachedir", "", "wazero 编译缓存目录（空=系统默认；用于验证预烤缓存）")
	flag.Parse()

	if *imagePath == "" || *gt == "" || *challenge == "" {
		fmt.Fprintln(os.Stderr, "Usage: gt-captcha-test -image <path> -gt <gt> -challenge <challenge>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// 读取图片
	imageData, err := os.ReadFile(*imagePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading image: %v\n", err)
		os.Exit(1)
	}

	// 创建求解器
	opts := []solver.Option{
		solver.WithPerf(*enablePerf),
		solver.WithConfThreshold(float32(*confThreshold)),
		solver.WithVerbose(*verbose),
	}
	if *wasmPath != "" {
		opts = append(opts, solver.WithWasmPath(*wasmPath))
	}
	if *cacheDir != "" {
		opts = append(opts, solver.WithCacheDir(*cacheDir))
	}

	// 启动计时
	startupStart := time.Now()
	s, err := solver.NewCaptchaSolver(opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating solver: %v\n", err)
		os.Exit(1)
	}
	startupMs := time.Since(startupStart).Milliseconds()
	defer func() { _ = s.Close() }()

	// 求解 (本地推理层)
	ctx := context.Background()
	res, err := s.Solve(ctx, imageData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error solving captcha: %v\n", err)
		os.Exit(1)
	}

	// 组装 W 参数 (协议加密层)
	points := make([][2]float64, len(res.Matches))
	for i, m := range res.Matches {
		points[i] = [2]float64{m.X, m.Y}
	}
	w, err := crypto.ClickCalculate(points, *gt, *challenge)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating w: %v\n", err)
		os.Exit(1)
	}

	// 打印关键结果
	fmt.Printf("W Parameter: %s\n", w)

	// 注入启动耗时
	if res.Perf == nil {
		res.Perf = &solver.PerfReport{}
	}
	res.Perf.StartupMs = startupMs

	// 输出详细结果
	if *verbose {
		output, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(output))
	}
}
