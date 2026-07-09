package solver

import "errors"

// 求解阶段的哨兵错误。均可用 errors.Is 判定；多数为「本张图内容/检测不理想」的语义，
// 调用方（如 pkg/client 的重试逻辑）据此判断换图重试是否可能有帮助。
var (
	// ErrImageDecode 表示输入图像无法解码（非 JPEG/PNG 或已损坏）。不可重试。
	ErrImageDecode = errors.New("gtlv/solver: image decode failed")
	// ErrNoPromptBox 表示未检出提示词框。
	ErrNoPromptBox = errors.New("gtlv/solver: no prompt box detected")
	// ErrNoAnswerBoxes 表示未检出任何答案框。
	ErrNoAnswerBoxes = errors.New("gtlv/solver: no answer boxes detected")
	// ErrAnswerCountOutOfRange 表示答案框数量不在 2-4 的合理区间（检测异常）。
	ErrAnswerCountOutOfRange = errors.New("gtlv/solver: answer box count out of range (want 2-4)")
	// ErrFeatureCountMismatch 表示特征数与检测框数不对齐（Rust 侧特征提取有静默跳过）。
	ErrFeatureCountMismatch = errors.New("gtlv/solver: feature/detection count mismatch")
	// ErrNoMatches 表示匹配阶段未产出任何匹配。
	ErrNoMatches = errors.New("gtlv/solver: no matches produced")
)
