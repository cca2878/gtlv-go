package classic

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestGenerateTrack(t *testing.T) {
	const distance = 120
	track := GenerateTrack(distance)

	if len(track) < 3 {
		t.Fatalf("track too short: %d", len(track))
	}
	// 第 2 个点是归零点 [0,0,0]。
	if track[1] != (TrackPoint{0, 0, 0}) {
		t.Errorf("track[1] = %v, want zero point", track[1])
	}

	// 从归零点起：X 单调不减、Y 恒 0、时间单调不减，末端逼近 distance。
	lastX, lastT := 0, 0
	maxX := 0
	for _, p := range track[1:] {
		if p[0] < lastX {
			t.Errorf("X 回退: %d -> %d", lastX, p[0])
		}
		if p[1] != 0 {
			t.Errorf("Y 应恒为 0，得到 %d", p[1])
		}
		if p[2] < lastT {
			t.Errorf("时间回退: %d -> %d", lastT, p[2])
		}
		lastX, lastT = p[0], p[2]
		if p[0] > maxX {
			maxX = p[0]
		}
	}
	if maxX > distance {
		t.Errorf("maxX %d 超过 distance %d", maxX, distance)
	}
	if maxX < distance*9/10 {
		t.Errorf("maxX %d 未逼近 distance %d", maxX, distance)
	}
}

func TestEncryptTrackFormat(t *testing.T) {
	enc := EncryptTrack(GenerateTrack(100))
	// gt 轨迹编码为 r!!i!!o 三段，故恰含两处 "!!"。
	if n := strings.Count(enc, "!!"); n != 2 {
		t.Fatalf("expected 2 '!!' separators, got %d in %q", n, enc)
	}
}

// TestIdentifyGapSynthetic 构造「完整图 vs 在 x≈130 处挖暗块的缺口图」，验证识别命中。
func TestIdentifyGapSynthetic(t *testing.T) {
	const w, h, gapX = 260, 160, 130
	full := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := image.NewRGBA(image.Rect(0, 0, w, h))
	base := color.RGBA{200, 200, 200, 255}
	for y := range h {
		for x := range w {
			full.Set(x, y, base)
			if x >= gapX && x < gapX+8 {
				bg.Set(x, y, color.RGBA{10, 10, 10, 255}) // 缺口暗块
			} else {
				bg.Set(x, y, base)
			}
		}
	}

	got := IdentifyGap(bg, full)
	if got < gapX-4 || got > gapX+4 {
		t.Fatalf("IdentifyGap = %d, want ≈ %d", got, gapX)
	}
}

func TestSolveSlideRejectsBadImage(t *testing.T) {
	if _, err := SolveSlide([]byte("not an image"), []byte("nope")); err == nil {
		t.Fatal("want decode error for garbage input")
	}
}
