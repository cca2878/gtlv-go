package classic

import (
	"fmt"

	gtimage "github.com/cca2878/gtlv-go/internal/image"
)

// SlideResult 是滑动验证码的本地求解结果。
type SlideResult struct {
	// Distance 是缺口相对滑块起点的水平位移（还原后 260 宽坐标系，像素）。
	Distance int
	// EncryptedTrack 是极验特有编码后的滑动轨迹。
	EncryptedTrack string
}

// SolveSlide 纯本地求解滑动验证码：
// 还原乱序背景（bg=带缺口、fullbg=完整）→ 缺口识别 → 生成并加密拟人轨迹。
// bgData/fullbgData 为原始（乱序）图字节（JPEG/PNG）。不触网。
func SolveSlide(bgData, fullbgData []byte) (*SlideResult, error) {
	bg, err := gtimage.Decode(bgData)
	if err != nil {
		return nil, fmt.Errorf("decode bg: %w", err)
	}
	fullbg, err := gtimage.Decode(fullbgData)
	if err != nil {
		return nil, fmt.Errorf("decode fullbg: %w", err)
	}

	restoredBg := RestoreImage(bg)
	restoredFull := RestoreImage(fullbg)

	distance := IdentifyGap(restoredBg, restoredFull)
	if distance <= 0 {
		return nil, fmt.Errorf("gap not found (distance=%d)", distance)
	}

	track := GenerateTrack(distance)
	return &SlideResult{
		Distance:       distance,
		EncryptedTrack: EncryptTrack(track),
	}, nil
}
