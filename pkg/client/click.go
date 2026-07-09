package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/cca2878/gtlv-go/pkg/crypto"
	"github.com/cca2878/gtlv-go/pkg/solver"
)

// clickValidate 走点选路径：取图 → 本地 wasm 求解 → 算 w → 补足时延 → 提交，失败可换图重试。
// 对应 biliTicker_gt 的 Click::simple_match(_retry)。
func (s *session) clickValidate(ctx context.Context, sol *solver.CaptchaSolver) (string, error) {
	picURL, err := s.getClickImage(ctx)
	if err != nil {
		return "", fmt.Errorf("get click image failed: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= s.c.maxAttempts; attempt++ {
		validate, retryable, err := s.clickAttempt(ctx, sol, picURL)
		if err == nil {
			return validate, nil
		}
		lastErr = err
		if !retryable || attempt == s.c.maxAttempts {
			break
		}
		// 换一张图重试（refresh.php，同 gt/challenge）。
		picURL, err = s.refreshClick(ctx)
		if err != nil {
			return "", fmt.Errorf("refresh failed: %w", err)
		}
	}
	return "", lastErr
}

// clickAttempt 执行一次点选尝试。retryable 表示该失败是否值得换图再试。
func (s *session) clickAttempt(ctx context.Context, sol *solver.CaptchaSolver, picURL string) (validate string, retryable bool, err error) {
	// 墙钟锚点：本轮验证码已签发，后续「求解 + 提交」需距此至少 verifyDelay。
	issued := time.Now()

	imgData, err := s.c.download(ctx, picURL)
	if err != nil {
		return "", false, fmt.Errorf("download image failed: %w", err)
	}

	res, err := sol.Solve(ctx, imgData)
	if err != nil {
		// 图内容/检测不理想，换图重试可能有帮助。
		return "", true, fmt.Errorf("solver failed: %w", err)
	}
	if len(res.Matches) == 0 {
		return "", true, fmt.Errorf("solver returned no matches")
	}

	points := make([][2]float64, len(res.Matches))
	for i, m := range res.Matches {
		points[i] = [2]float64{m.X, m.Y}
	}
	w, err := crypto.ClickCalculate(points, s.gt, s.challenge)
	if err != nil {
		// 加密是确定性的，换图无益。
		return "", false, fmt.Errorf("w calculation failed: %w", err)
	}

	if err := s.sleepUntil(ctx, issued); err != nil {
		return "", false, err
	}

	validate, err = s.verify(ctx, s.challenge, w)
	if err != nil {
		return "", errors.Is(err, ErrVerificationFailed), err
	}
	return validate, false, nil
}

// getClickImage 取本轮点选图片参数并拼出图片 URL。对应 click.rs::get_new_c_s_args。
func (s *session) getClickImage(ctx context.Context) (string, error) {
	data, err := s.c.getJSONP(ctx, "http://"+s.c.visitHost+"/get.php", newImageQuery(s.gt, s.challenge, "click"))
	if err != nil {
		return "", err
	}
	return joinImageURL(data, "static_servers", "pic")
}

// refreshClick 请求换一张点选图（同 gt/challenge）。对应 click.rs::refresh。
func (s *session) refreshClick(ctx context.Context) (string, error) {
	q := url.Values{}
	q.Set("gt", s.gt)
	q.Set("challenge", s.challenge)
	data, err := s.c.getJSONP(ctx, "http://"+s.c.visitHost+"/refresh.php", q)
	if err != nil {
		return "", err
	}
	return joinImageURL(data, "image_servers", "pic")
}
