package classic

import (
	"math"
	"math/rand"
	"time"
)

// TrackPoint 表示轨迹中的一个点 [x, y, t]
type TrackPoint [3]int

// GenerateTrack 生成模拟人类行为的滑动轨迹。
// 忠实移植自 biliTicker_gt 的 get_slide_track：指数缓动的 X 位移、随机时间步进，
// Y 恒为 0（仅起点带负向噪声）。刻意不加额外抖动，以匹配已验证通过的参考实现。
func GenerateTrack(distance int) []TrackPoint {
	if distance <= 0 {
		return []TrackPoint{{0, 0, 0}}
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	track := make([]TrackPoint, 0)

	// 1. 初始化起点噪声（x、y 均取 -50..-10）。
	x1 := r.Intn(41) - 50
	y1 := r.Intn(41) - 50
	track = append(track, TrackPoint{x1, y1, 0})
	track = append(track, TrackPoint{0, 0, 0})

	// 2. 步数与初始时间。
	count := 30 + (distance / 2)
	t := r.Intn(51) + 50 // 50..100

	lastX := 0
	for i := range count {
		sep := float64(i) / float64(count)
		// 指数缓动曲线（sep ∈ [0,1)，恒走 else 分支）。
		x := (1.0 - math.Pow(2, -10.0*sep)) * float64(distance)
		ix := int(math.Round(x))

		t += r.Intn(11) + 10 // 10..20ms 间隔

		if ix == lastX {
			continue
		}
		track = append(track, TrackPoint{ix, 0, t}) // Y 恒 0
		lastX = ix
	}

	// 3. 末点重复一次（符合 gt 特征）。
	if len(track) > 0 {
		track = append(track, track[len(track)-1])
	}

	return track
}

// EncryptTrack 对轨迹进行 gt 特有的编码。
func EncryptTrack(track []TrackPoint) string {
	if len(track) < 2 {
		return ""
	}

	// 1. 计算偏移量
	type step struct {
		dx, dy, dt int
	}
	steps := make([]step, 0)
	accumulatedDt := 0

	for i := 0; i < len(track)-1; i++ {
		dx := track[i+1][0] - track[i][0]
		dy := track[i+1][1] - track[i][1]
		dt := track[i+1][2] - track[i][2]

		if dx == 0 && dy == 0 && dt == 0 {
			continue
		}

		if dx == 0 && dy == 0 {
			accumulatedDt += dt
		} else {
			steps = append(steps, step{dx, dy, dt + accumulatedDt})
			accumulatedDt = 0
		}
	}
	if accumulatedDt != 0 && len(steps) > 0 {
		steps = append(steps, step{steps[len(steps)-1].dx, steps[len(steps)-1].dy, accumulatedDt})
	}

	// 2. 编码逻辑
	const charset = "()*,-./0123456789:?@ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqr"
	n := len(charset)

	encodeValue := func(v int) string {
		absV := int(math.Abs(float64(v)))
		o := absV / n
		if o >= n {
			o = n - 1
		}

		res := ""
		if v < 0 {
			res += "!"
		}
		if o > 0 {
			res += "$" + string(charset[o])
		}
		res += string(charset[absV%n])
		return res
	}

	pairs := []struct {
		dx, dy int
		char   rune
	}{
		{1, 0, 's'}, {2, 0, 't'}, {1, -1, 'u'}, {1, 1, 'v'}, {0, 1, 'w'},
		{0, -1, 'x'}, {3, 0, 'y'}, {2, -1, 'z'}, {2, 1, '~'},
	}

	var r, i, o string
	for _, s := range steps {
		foundPair := false
		for _, p := range pairs {
			if s.dx == p.dx && s.dy == p.dy {
				i += string(p.char)
				foundPair = true
				break
			}
		}

		if !foundPair {
			r += encodeValue(s.dx)
			i += encodeValue(s.dy)
		}
		o += encodeValue(s.dt)
	}

	return r + "!!" + i + "!!" + o
}
