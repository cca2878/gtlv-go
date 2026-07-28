// Command gt-captcha-e2e 跑一次真实的零到 validate 端到端点选验证：
// 从登记端点取 gt/challenge → 拉图 → 本地求解 → 算 w → 提交 → 打印 validate。
//
// 用法:
//
//	go run ./cmd/gt-captcha-e2e（模型随 wasm 内嵌）
//	go run ./cmd/gt-captcha-e2e -register <URL> -cachedir /tmp/gtlv -n 3
//
// 需联网。默认登记端点为公开的 Bilibili 点选验证码接口，仅供冒烟测试。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cca2878/gtlv-go/pkg/client"
	"github.com/cca2878/gtlv-go/pkg/solver"
)

func main() {
	var (
		registerURL = flag.String("register", client.BilibiliRegisterURL, "登记端点（返回 data.geetest.{gt,challenge}）")
		cacheDir    = flag.String("cachedir", "", "wazero 编译缓存目录（留空用系统缓存目录）")
		rounds      = flag.Int("n", 1, "连跑轮数")
		attempts    = flag.Int("attempts", 1, "单轮最多尝试次数（失败换图重试）")
		timeout     = flag.Duration("timeout", 60*time.Second, "单轮超时")
	)
	flag.Parse()

	// Solver 只建一次（模型热态常驻）。
	opts := []solver.Option{}
	if *cacheDir != "" {
		opts = append(opts, solver.WithCacheDir(*cacheDir))
	}
	fmt.Fprintln(os.Stderr, "初始化 solver（首次含 wasm 编译，可能数秒）...")
	initStart := time.Now()
	s, err := solver.NewCaptchaSolver(opts...)
	if err != nil {
		fatalf("solver 初始化失败: %v", err)
	}
	defer func() { _ = s.Close() }()
	fmt.Fprintf(os.Stderr, "solver 就绪，用时 %v\n\n", time.Since(initStart).Round(time.Millisecond))

	// V3Client 可复用：配置一次，对每轮不同 gt/challenge 反复调用。
	c := client.NewV3Client(client.WithMaxAttempts(*attempts))

	ok := 0
	for i := 1; i <= *rounds; i++ {
		if err := runOnce(i, *registerURL, c, s, *timeout); err != nil {
			fmt.Fprintf(os.Stderr, "[%d/%d] 失败: %v\n\n", i, *rounds, err)
			continue
		}
		ok++
	}

	fmt.Fprintf(os.Stderr, "完成: %d/%d 成功\n", ok, *rounds)
	if ok == 0 {
		os.Exit(1)
	}
}

func runOnce(i int, registerURL string, c *client.V3Client, s *solver.CaptchaSolver, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()

	gt, challenge, err := client.Register(ctx, nil, registerURL)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[%d] gt=%s challenge=%s\n", i, gt, challenge)

	validate, err := c.GetValidate(ctx, gt, challenge, s)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "[%d] validate=%s (%v)\n\n", i, validate, time.Since(start).Round(time.Millisecond))
	// validate 打到 stdout，便于脚本采集。
	fmt.Println(validate)
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
