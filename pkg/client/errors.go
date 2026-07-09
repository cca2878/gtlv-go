package client

import (
	"errors"
	"fmt"
)

// 哨兵错误，供 errors.Is 判定。
var (
	// ErrVerificationFailed 表示极验判定本次验证未通过（答案错误或行为可疑）。
	// 这是【可重试】信号：换一张图再来一次通常能成功（见 WithMaxAttempts）。
	// 具体服务端原因由 *VerifyError 携带，且 errors.Is(err, ErrVerificationFailed) 为真。
	ErrVerificationFailed = errors.New("gtlv/client: verification failed")

	// ErrSolverRequired 表示识别为点选，但调用 GetValidate 时未提供求解器。
	ErrSolverRequired = errors.New("gtlv/client: click captcha requires a solver")
)

// VerifyError 携带极验 verify 响应中的失败详情，Unwrap 到 ErrVerificationFailed。
type VerifyError struct {
	Result  string // data.result（点选路径；如 "fail"）
	Message string // data.message（滑动路径）
}

func (e *VerifyError) Error() string {
	switch {
	case e.Result != "" && e.Message != "":
		return fmt.Sprintf("gtlv/client: verification failed (result=%q, message=%q)", e.Result, e.Message)
	case e.Result != "":
		return fmt.Sprintf("gtlv/client: verification failed (result=%q)", e.Result)
	case e.Message != "":
		return fmt.Sprintf("gtlv/client: verification failed (message=%q)", e.Message)
	default:
		return "gtlv/client: verification failed (empty validate)"
	}
}

// Unwrap 让 errors.Is(err, ErrVerificationFailed) 成立。
func (e *VerifyError) Unwrap() error { return ErrVerificationFailed }

// UnsupportedCaptchaTypeError 表示服务端下发了本库不支持的验证码类型。
type UnsupportedCaptchaTypeError struct {
	Type string
}

func (e *UnsupportedCaptchaTypeError) Error() string {
	return fmt.Sprintf("gtlv/client: unsupported captcha type %q (want click or slide)", e.Type)
}
