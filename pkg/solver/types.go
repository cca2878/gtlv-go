// Package solver 提供验证码求解的核心 API。
package solver

// Detection 表示一个检测框。
type Detection struct {
	Box        BoundingBox `json:"box"`
	Confidence float64     `json:"confidence"`
	ClassID    int         `json:"class_id"` // 0=答案目标, 1=提示词
}

// BoundingBox 表示一个边界框。
type BoundingBox struct {
	XMin float64 `json:"x_min"`
	YMin float64 `json:"y_min"`
	XMax float64 `json:"x_max"`
	YMax float64 `json:"y_max"`
}

// Match 表示一个匹配结果。
type Match struct {
	PromptIndex int     `json:"prompt_index"`
	AnswerIndex int     `json:"answer_index"`
	Confidence  float64 `json:"confidence"`
	X           float64 `json:"x"` // 像素坐标
	Y           float64 `json:"y"` // 像素坐标
}

// Result 表示验证码求解的结果。
type Result struct {
	PromptBox      *BoundingBox `json:"prompt_box,omitempty"`
	Detections     []Detection  `json:"detections"`
	AnswerFeatures [][]float32  `json:"answer_features,omitempty"` // 答案框特征（verbose 模式）
	PromptFeatures [][]float32  `json:"prompt_features,omitempty"` // 提示词段特征（verbose 模式）
	Matches        []Match      `json:"matches"`
	Perf           *PerfReport  `json:"perf,omitempty"`
}

// PerfReport 表示性能耗时报告。
type PerfReport struct {
	// 启动耗时（dlopen 库 + gt_init 加载模型）
	StartupMs int64 `json:"startup_ms"`
	// Go 侧耗时
	ImageDecodeMs int64 `json:"image_decode_ms"`
	MatchMs       int64 `json:"match_ms"`
	CryptoMs      int64 `json:"crypto_ms"`
	// Rust 侧耗时（随 FFI wire 缓冲传回）
	RustPreprocessMs int64 `json:"rust_preprocess_ms"`
	YoloInferMs      int64 `json:"yolo_infer_ms"`
	CropResizeMs     int64 `json:"crop_resize_ms"`
	SiameseInferMs   int64 `json:"siamese_infer_ms"`
	RustTotalMs      int64 `json:"rust_total_ms"`
	// 总耗时
	TotalMs int64 `json:"total_ms"`
}
