package client

import (
	"context"
	"time"

	"github.com/cca2878/gtlv-go/pkg/crypto"
	"github.com/cca2878/gtlv-go/pkg/solver"
)

// MineResult 是 MineOnce 的观测结果：本轮点选的求解结果、是否通过、以及原图字节。
// 供训练数据挖掘：PASS 时可据 Result 的框把提示段/答案 tile 从 Image 裁出。
type MineResult struct {
	Passed   bool           // verify 是否通过（服务器判定点选正确）
	Image    []byte         // 本轮点选图原始字节
	Result   *solver.Result // 本地求解结果（检测框/提示框/匹配）；solve 失败时为 nil
	SolveErr string         // 求解失败原因（solve 报错/无匹配）；求解成功为空 → 空且未 Passed 即 verify 失败
}

// MineOnce 对给定 gt/challenge 跑一次点选流程（单图、单次提交，不换图重试），
// 返回求解结果与 pass/fail + 原图。用于采集服务器验证过的训练对，非常规求解入口
// （常规求解用 GetValidate）。识别为非点选时返回 UnsupportedCaptchaTypeError。
func (c *V3Client) MineOnce(ctx context.Context, gt, challenge string, sol *solver.CaptchaSolver) (*MineResult, error) {
	s := &session{c: c, gt: gt, challenge: challenge}
	if _, _, err := s.getCS(ctx); err != nil {
		return nil, err
	}
	typ, err := s.getType(ctx)
	if err != nil {
		return nil, err
	}
	if typ != "click" {
		return nil, &UnsupportedCaptchaTypeError{Type: typ}
	}
	picURL, err := s.getClickImage(ctx)
	if err != nil {
		return nil, err
	}
	issued := time.Now()
	img, err := s.c.download(ctx, picURL)
	if err != nil {
		return nil, err
	}
	res, solveErr := sol.Solve(ctx, img)
	if solveErr != nil {
		return &MineResult{Passed: false, Image: img, SolveErr: solveErr.Error()}, nil
	}
	if len(res.Matches) == 0 {
		return &MineResult{Passed: false, Image: img, Result: res, SolveErr: "no_matches"}, nil
	}
	points := make([][2]float64, len(res.Matches))
	for i, m := range res.Matches {
		points[i] = [2]float64{m.X, m.Y}
	}
	w, err := crypto.ClickCalculate(points, gt, challenge)
	if err != nil {
		return nil, err
	}
	if err := s.sleepUntil(ctx, issued); err != nil {
		return nil, err
	}
	_, verr := s.verify(ctx, challenge, w)
	return &MineResult{Passed: verr == nil, Image: img, Result: res}, nil
}
