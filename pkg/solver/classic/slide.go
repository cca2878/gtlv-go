// Package classic 是滑块/轨迹类验证码的【本地】辅助：背景图还原、缺口识别、滑动轨迹生成/加密。
// 纯本地图像处理与计算，不触网。
//
// 许可：本包逻辑移植自 biliTicker_gt（AGPL-3.0，https://github.com/Amorter/biliTicker_gt
// 的 src/slide.rs），是本库整体采用 AGPL-3.0 的原因（见 README「许可」）。
package classic

import (
	"image"
	"image/draw"
	"math"
)

// RestoreImage 还原极验乱序背景图。
// 极验背景图由 52 个切片组成，每个切片宽 10 像素，高 80 像素。
// 原始图宽 312/320，还原后宽 260，高 160。
func RestoreImage(img image.Image) image.Image {
	offset := []int{
		39, 38, 48, 49, 41, 40, 46, 47, 35, 34, 50, 51, 33, 32, 28, 29, 27, 26, 36, 37, 31, 30,
		44, 45, 43, 42, 12, 13, 23, 22, 14, 15, 21, 20, 8, 9, 25, 24, 6, 7, 3, 2, 0, 1, 11, 10,
		4, 5, 19, 18, 16, 17,
	}

	restored := image.NewRGBA(image.Rect(0, 0, 260, 160))
	const wSep = 10
	const hSep = 80

	for i, off := range offset {
		// 源坐标计算 (12 像素步长)
		sx := (off % 26) * 12
		sy := 0
		if off > 25 {
			sy = hSep
		}

		// 目标坐标计算 (10 像素步长)
		dx := (i % 26) * 10
		dy := 0
		if i > 25 {
			dy = hSep
		}

		// 拷贝切片
		srcRect := image.Rect(sx, sy, sx+wSep, sy+hSep)
		draw.Draw(restored, image.Rect(dx, dy, dx+wSep, dy+hSep), img, srcRect.Min, draw.Src)
	}

	return restored
}

// IdentifyGap 识别缺口位置。
// 比较带缺口图 (bg) 和 完整图 (fullBg)，找到像素差异最大的 X 轴起点。
func IdentifyGap(bg, fullBg image.Image) int {
	bounds := bg.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	// 简单的像素差异法
	// 通常缺口在 X > 40 的位置
	const minX = 40
	maxDiff := 0.0
	bestX := 0

	for x := minX; x < width-40; x++ {
		totalDiff := 0.0
		for y := range height {
			r1, g1, b1, _ := bg.At(x, y).RGBA()
			r2, g2, b2, _ := fullBg.At(x, y).RGBA()

			diff := math.Abs(float64(int(r1>>8)-int(r2>>8))) +
				math.Abs(float64(int(g1>>8)-int(g2>>8))) +
				math.Abs(float64(int(b1>>8)-int(b2>>8)))

			if diff > 50 { // 阈值过滤噪声
				totalDiff += diff
			}
		}

		if totalDiff > maxDiff {
			maxDiff = totalDiff
			bestX = x
		}
	}

	return bestX
}
