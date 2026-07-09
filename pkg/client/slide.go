package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cca2878/gtlv-go/pkg/crypto"
	"github.com/cca2878/gtlv-go/pkg/solver/classic"
)

// slideParams 是滑动 get.php 本轮返回的参数。
type slideParams struct {
	c            []byte
	s            string
	newChallenge string // 滑动会下发新的 challenge，后续算 w 与 verify 均用它
	fullbgURL    string // 完整背景图
	bgURL        string // 带缺口背景图（乱序）
}

// slideValidate 走滑动路径：取参数与图 → 本地还原+识别+轨迹 → 算 w → 补足时延 → 提交，
// 失败可重取新一轮重试。对应 biliTicker_gt 的 Slide::test。滑动为纯本地图像处理，不需 wasm 求解器。
func (s *session) slideValidate(ctx context.Context) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= s.c.maxAttempts; attempt++ {
		// 滑动无独立 refresh 端点；每次尝试都取新一轮参数（新 challenge + 新图）。
		p, err := s.getSlideParams(ctx)
		if err != nil {
			return "", fmt.Errorf("get slide params failed: %w", err)
		}
		validate, retryable, err := s.slideAttempt(ctx, p)
		if err == nil {
			return validate, nil
		}
		lastErr = err
		if !retryable || attempt == s.c.maxAttempts {
			break
		}
	}
	return "", lastErr
}

// slideAttempt 执行一次滑动尝试。retryable 表示该失败是否值得重取新图再试。
func (s *session) slideAttempt(ctx context.Context, p *slideParams) (validate string, retryable bool, err error) {
	// 墙钟锚点：本轮验证码已签发，后续「求解 + 提交」需距此至少 verifyDelay。
	issued := time.Now()

	bgData, err := s.c.download(ctx, p.bgURL)
	if err != nil {
		return "", false, fmt.Errorf("download bg failed: %w", err)
	}
	fullbgData, err := s.c.download(ctx, p.fullbgURL)
	if err != nil {
		return "", false, fmt.Errorf("download fullbg failed: %w", err)
	}

	sol, err := classic.SolveSlide(bgData, fullbgData)
	if err != nil {
		// 缺口识别不理想，换图重试可能有帮助。
		return "", true, fmt.Errorf("slide solve failed: %w", err)
	}

	// 滑动的 w 用本轮下发的新 challenge，且依赖 c/s。
	w, err := crypto.SlideCalculate(sol.Distance, sol.EncryptedTrack, s.gt, p.newChallenge, p.c, p.s)
	if err != nil {
		return "", false, fmt.Errorf("w calculation failed: %w", err)
	}

	if err := s.sleepUntil(ctx, issued); err != nil {
		return "", false, err
	}

	validate, err = s.verify(ctx, p.newChallenge, w)
	if err != nil {
		return "", errors.Is(err, ErrVerificationFailed), err
	}
	return validate, false, nil
}

// getSlideParams 取本轮滑动参数（c/s、新 challenge、fullbg/bg 图 URL）。
// 对应 slide.rs::get_new_c_s_args。
func (s *session) getSlideParams(ctx context.Context) (*slideParams, error) {
	data, err := s.c.getJSONP(ctx, "http://"+s.c.visitHost+"/get.php", newImageQuery(s.gt, s.challenge, "slide"))
	if err != nil {
		return nil, err
	}
	cBytes, err := jsonBytes(data, "c")
	if err != nil {
		return nil, err
	}
	str, err := jsonString(data, "s")
	if err != nil {
		return nil, err
	}
	newChallenge, err := jsonString(data, "challenge")
	if err != nil {
		return nil, err
	}
	fullbgURL, err := joinImageURL(data, "static_servers", "fullbg")
	if err != nil {
		return nil, err
	}
	bgURL, err := joinImageURL(data, "static_servers", "bg")
	if err != nil {
		return nil, err
	}
	return &slideParams{
		c:            cBytes,
		s:            str,
		newChallenge: newChallenge,
		fullbgURL:    fullbgURL,
		bgURL:        bgURL,
	}, nil
}
