package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// BilibiliRegisterURL 是一个可用于自测的公开点选验证码登记端点：返回一对 gt/challenge。
// 仅供联调/冒烟测试；正式使用时 gt/challenge 应来自各自的业务接口。
const BilibiliRegisterURL = "https://passport.bilibili.com/x/passport-login/captcha?source=main_web"

// Register 从业务登记端点获取一对 gt/challenge（点选 bootstrap）。
// 期望响应为 JSON，gt/challenge 位于 data.geetest 下（对齐 biliTicker_gt 的 Click::register_test）。
// hc 为 nil 时用带 15s 超时的默认 client。
func Register(ctx context.Context, hc *http.Client, registerURL string) (gt, challenge string, err error) {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registerURL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("bad status: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	var root struct {
		Data struct {
			Gt struct {
				GT        string `json:"gt"`
				Challenge string `json:"challenge"`
			} `json:"geetest"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return "", "", fmt.Errorf("parse register json: %w (body=%.160q)", err, body)
	}
	g := root.Data.Gt
	if g.GT == "" || g.Challenge == "" {
		return "", "", fmt.Errorf("register response missing gt/challenge (body=%.160q)", body)
	}
	return g.GT, g.Challenge, nil
}
