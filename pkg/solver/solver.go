// Package solver 是本库的【纯本地推理层】——不触网，离线可测。
//
// CaptchaSolver 把图像字节求解为点选坐标：图像解码校验 → wazero 进程内加载的 wasm 推理
// （检测 + 特征）→ NMS/TopK 解析 → 匈牙利匹配 → 坐标。推理后端与 Solve 管线解耦（Solve 只
// 依赖一个 infer 函数）。内嵌 wasm 与可选的 AOT 编译缓存均经 go:embed 压缩携带、启动时解压/播种。
//
// 网络编排（拉图/提交/validate）属另一层 pkg/client，不在本包。
package solver

import (
	"context"
	"fmt"
	"time"

	gtimage "github.com/cca2878/gtlv-go/internal/image"
	"github.com/cca2878/gtlv-go/internal/matcher"
	"github.com/cca2878/gtlv-go/internal/perf"
)

// DefaultConfThreshold 是未经 WithConfThreshold 指定时的检测置信度阈值。
//
// 检测分数近乎二值：真答案格多在 0.8 以上，误检要到 0.005 以下才出现。取值落在这段空档里，
// 以召回笔画稀疏或贴边被截断的格；超量由 Rust 侧的答案格数量上限截断兜底。
const DefaultConfThreshold float32 = 0.1

// Option 是 CaptchaSolver 的配置选项。
type Option func(*options)

type options struct {
	wasmPath      string
	cacheDir      string
	enablePerf    bool
	verbose       bool
	confThreshold float32
}

// WithWasmPath 指定外部 .wasm 模块路径（可选；缺省用内嵌 go:embed 的模块）。
func WithWasmPath(path string) Option {
	return func(o *options) {
		o.wasmPath = path
	}
}

// WithCacheDir 指定 wazero 编译缓存目录（可选；缺省用系统缓存目录）。
// 持久化缓存让二次启动跳过 wasm→机器码的编译（首启数秒，之后近乎免除）。
func WithCacheDir(dir string) Option {
	return func(o *options) {
		o.cacheDir = dir
	}
}

// WithPerf 启用性能计时。
func WithPerf(enable bool) Option {
	return func(o *options) {
		o.enablePerf = enable
	}
}

// WithConfThreshold 设置置信度阈值。
func WithConfThreshold(threshold float32) Option {
	return func(o *options) {
		o.confThreshold = threshold
	}
}

// WithVerbose 启用详细输出（包含特征向量等调试信息）。
func WithVerbose(enable bool) Option {
	return func(o *options) {
		o.verbose = enable
	}
}

// CaptchaSolver 是验证码求解器。推理经 WASM（wazero 进程内加载 Rust wasm 模块，无 CGO）完成。
//
// 并发安全：Solve 可被多个 goroutine 同时调用——底层 wasm 推理经内部互斥串行化
// （验证码低频，串行足矣）。Close 后不得再调用 Solve。
type CaptchaSolver struct {
	opts options
	// infer 是推理后端：调用 wasm，返回解码后的推理结果。Solve 只依赖此字段，与后端解耦。
	infer func(ctx context.Context, image []byte, conf float32) (*wireResponse, error)
	// onClose 释放后端资源（关闭 wazero 运行时）。
	onClose func() error
}

// NewCaptchaSolver 创建求解器实例：实例化 wasm 模块、一次性加载模型（热态常驻）。
// 模型已随 wasm 模块内嵌（由 gtlv-core include_bytes! 携带），无需任何外部文件。
// 换模型＝换 wasm：改 gtlv-core/modeldata 后重编，或用 WithWasmPath 指定外部 .wasm。
func NewCaptchaSolver(opts ...Option) (*CaptchaSolver, error) {
	o := options{
		enablePerf:    true,
		confThreshold: DefaultConfThreshold,
	}
	for _, opt := range opts {
		opt(&o)
	}
	backend, err := newWasmBackend(context.Background(), o)
	if err != nil {
		return nil, err
	}

	return &CaptchaSolver{
		opts:    o,
		infer:   backend.infer,
		onClose: backend.close,
	}, nil
}

// Close 释放 FFI 后端资源。
func (s *CaptchaSolver) Close() error {
	if s.onClose != nil {
		return s.onClose()
	}
	return nil
}

// Solve 解决验证码。image: JPEG/PNG 图像字节。
func (s *CaptchaSolver) Solve(ctx context.Context, imageData []byte) (*Result, error) {
	timer := perf.New(s.opts.enablePerf)
	totalStart := time.Now()

	// 1. 只校验图像可解码（读文件头即可）；真正的解码在 wasm 侧、且只做一次。
	timer.Start("image_decode")
	if err := gtimage.Validate(imageData); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrImageDecode, err)
	}
	timer.Stop("image_decode")

	// 2. FFI 推理：检测 + 特征提取
	timer.Start("rust_inference")
	resp, err := s.infer(ctx, imageData, s.opts.confThreshold)
	if err != nil {
		return nil, fmt.Errorf("inference failed: %w", err)
	}
	timer.Stop("rust_inference")

	// 3. 解析检测结果（Rust 侧已做过 NMS + Top-K 过滤，响应中只有过滤后的结果）
	var promptBox *BoundingBox
	var answerBoxes []BoundingBox
	detections := make([]Detection, 0, len(resp.Detections))

	for _, det := range resp.Detections {
		box := BoundingBox{
			XMin: float64(det.Box.XMin),
			YMin: float64(det.Box.YMin),
			XMax: float64(det.Box.XMax),
			YMax: float64(det.Box.YMax),
		}

		if det.ClassID == 0 { // 答案目标（已 NMS + Top-K 过滤）
			answerBoxes = append(answerBoxes, box)
		}

		detections = append(detections, Detection{
			Box:        box,
			Confidence: float64(det.Confidence),
			ClassID:    int(det.ClassID),
		})
	}

	// 答案框特征
	answerFeatures := make([][]float64, 0, len(resp.AnswerFeatures))
	for _, fv := range resp.AnswerFeatures {
		answerFeatures = append(answerFeatures, float64Slice(fv))
	}

	// 提示词框（Rust 侧已选最高置信度）
	if resp.PromptBox != nil {
		promptBox = &BoundingBox{
			XMin: float64(resp.PromptBox.XMin),
			YMin: float64(resp.PromptBox.YMin),
			XMax: float64(resp.PromptBox.XMax),
			YMax: float64(resp.PromptBox.YMax),
		}
	}

	// 4. 拆分提示词框并提取特征
	if promptBox == nil {
		return nil, ErrNoPromptBox
	}

	m := len(answerBoxes)
	if m == 0 {
		return nil, ErrNoAnswerBoxes
	}

	// 5. 矩形指派。k(提示词字数)= Rust 按提示框长宽比标定后返回的提示特征数，不再等于 m。
	timer.Start("match")
	promptFeatures := make([][]float64, 0, len(resp.PromptFeatures))
	for _, fv := range resp.PromptFeatures {
		promptFeatures = append(promptFeatures, float64Slice(fv))
	}
	k := len(promptFeatures)
	if k < 2 || k > 4 {
		// k 由提示框 aspect 标定；落在 [2,4] 外说明提示框缺失/异常或提示段特征提取失败。
		return nil, fmt.Errorf("%w: got k=%d from prompt box", ErrAnswerCountOutOfRange, k)
	}
	if m < k {
		// 答案格数不足以覆盖 k 个目标 → 检测漏了目标，换图重试。
		return nil, fmt.Errorf("%w: %d tiles < %d targets", ErrAnswerCountOutOfRange, m, k)
	}

	// 提示词框按 k 等分（用于展示/回映）
	promptSegments := splitPromptBox(promptBox, k)

	// 校验答案特征数与答案框对齐（Rust 侧特征提取失败会静默跳过）
	if len(answerFeatures) != m {
		return nil, fmt.Errorf("%w: %d answer features for %d detections",
			ErrFeatureCountMismatch, len(answerFeatures), m)
	}

	var matches []Match
	if len(promptFeatures) > 0 && len(answerFeatures) > 0 {
		rowIndices, colIndices, costMatrix := matcher.MatchWithCost(promptFeatures, answerFeatures)

		for i, rowIdx := range rowIndices {
			colIdx := colIndices[i]
			if rowIdx < len(promptSegments) && colIdx < len(answerBoxes) {
				ansBox := answerBoxes[colIdx]
				xc := (ansBox.XMin + ansBox.XMax) / 2
				yc := (ansBox.YMin + ansBox.YMax) / 2
				dist := 0.0
				if rowIdx < len(costMatrix) && colIdx < len(costMatrix[rowIdx]) {
					dist = costMatrix[rowIdx][colIdx]
				}
				matches = append(matches, Match{
					PromptIndex: rowIdx,
					AnswerIndex: colIdx,
					Confidence:  1.0 / (1.0 + dist), // 将距离转换为置信度
					X:           xc,
					Y:           yc,
				})
			}
		}
	}
	timer.Stop("match")

	// 6. 构建结果
	result := &Result{
		PromptBox:  promptBox,
		Detections: detections,
		Matches:    matches,
	}

	// verbose 模式下附加完整特征向量（调试用）
	if s.opts.verbose {
		result.AnswerFeatures = append([][]float32(nil), resp.AnswerFeatures...)
		result.PromptFeatures = append([][]float32(nil), resp.PromptFeatures...)
	}

	// 7. 性能报告
	if s.opts.enablePerf {
		result.Perf = &PerfReport{
			ImageDecodeMs:    timer.Elapsed("image_decode"),
			MatchMs:          timer.Elapsed("match"),
			RustPreprocessMs: resp.Perf.PreprocessMs,
			YoloInferMs:      resp.Perf.YoloInferMs,
			CropResizeMs:     resp.Perf.CropResizeMs,
			SiameseInferMs:   resp.Perf.SiameseInferMs,
			RustTotalMs:      resp.Perf.TotalMs,
			TotalMs:          time.Since(totalStart).Milliseconds(),
		}
	}

	return result, nil
}

// splitPromptBox 将提示词整体框水平等分为 n 份。
func splitPromptBox(box *BoundingBox, n int) []BoundingBox {
	if n <= 0 {
		return nil
	}
	segments := make([]BoundingBox, n)
	segWidth := (box.XMax - box.XMin) / float64(n)
	for i := range n {
		segments[i] = BoundingBox{
			XMin: box.XMin + float64(i)*segWidth,
			YMin: box.YMin,
			XMax: box.XMin + float64(i+1)*segWidth,
			YMax: box.YMax,
		}
	}
	return segments
}

func float64Slice(vals []float32) []float64 {
	result := make([]float64, len(vals))
	for i, v := range vals {
		result[i] = float64(v)
	}
	return result
}
