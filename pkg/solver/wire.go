package solver

import (
	"encoding/binary"
	"fmt"
	"math"
)

// wireMagic 与 Rust 侧 wire.rs 的 MAGIC 常量一致。改动布局须同步 bump 两侧。
// 校验它可在共享库陈旧 / 布局漂移时响亮失败，而非静默解出垃圾。
var wireMagic = [4]byte{'G', 'T', 'C', '1'}

// wire* 结构体镜像 Rust 侧 wire.rs 的推理结果。字段为 float32/int32，与线格式一致；
// 上层 solver 负责转 float64。
type wireBox struct {
	XMin, YMin, XMax, YMax float32
}

type wireDetection struct {
	Box        wireBox
	Confidence float32
	ClassID    int32
}

type wirePerf struct {
	PreprocessMs, YoloInferMs, CropResizeMs, SiameseInferMs, TotalMs int64
}

type wireResponse struct {
	Detections     []wireDetection
	PromptBox      *wireBox
	AnswerFeatures [][]float32
	PromptFeatures [][]float32
	Perf           wirePerf
}

// wireReader 是带边界检查的小端游标：任何越界读都返回 error，绝不 panic。
type wireReader struct {
	b   []byte
	off int
}

func (r *wireReader) take(n int) ([]byte, error) {
	if n < 0 || r.off+n > len(r.b) {
		return nil, fmt.Errorf("wire: short buffer: need %d bytes at offset %d of %d", n, r.off, len(r.b))
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s, nil
}

func (r *wireReader) u8() (byte, error) {
	s, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return s[0], nil
}

func (r *wireReader) u32() (uint32, error) {
	s, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(s), nil
}

func (r *wireReader) f32() (float32, error) {
	v, err := r.u32()
	return math.Float32frombits(v), err
}

func (r *wireReader) i64() (int64, error) {
	s, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(s)), nil
}

func (r *wireReader) box() (wireBox, error) {
	var b wireBox
	var err error
	if b.XMin, err = r.f32(); err != nil {
		return b, err
	}
	if b.YMin, err = r.f32(); err != nil {
		return b, err
	}
	if b.XMax, err = r.f32(); err != nil {
		return b, err
	}
	if b.YMax, err = r.f32(); err != nil {
		return b, err
	}
	return b, nil
}

// features 读取一组特征向量：u32 数量，其后每条为 u32 维度 + 维度个 f32。
func (r *wireReader) features() ([][]float32, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	out := make([][]float32, 0, n)
	for i := range n {
		dim, err := r.u32()
		if err != nil {
			return nil, err
		}
		// 先按缓冲边界校验，避免因损坏的维度值预分配巨块内存。
		if _, err := r.take(int(dim) * 4); err != nil {
			return nil, fmt.Errorf("wire: feature %d dim %d: %w", i, dim, err)
		}
		r.off -= int(dim) * 4 // 回退，逐个读取
		v := make([]float32, dim)
		for j := range v {
			if v[j], err = r.f32(); err != nil {
				return nil, err
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// decodeWireResponse 解析 Rust 侧 wire.rs 编码的推理结果。布局见 wire.rs 文档。
func decodeWireResponse(b []byte) (*wireResponse, error) {
	r := &wireReader{b: b}

	magic, err := r.take(4)
	if err != nil {
		return nil, fmt.Errorf("wire: %w", err)
	}
	if magic[0] != wireMagic[0] || magic[1] != wireMagic[1] || magic[2] != wireMagic[2] || magic[3] != wireMagic[3] {
		return nil, fmt.Errorf("wire: bad magic %q (want %q); stale/incompatible library?", magic, wireMagic[:])
	}

	nDet, err := r.u32()
	if err != nil {
		return nil, err
	}
	resp := &wireResponse{Detections: make([]wireDetection, 0, nDet)}
	for range nDet {
		var d wireDetection
		if d.Box, err = r.box(); err != nil {
			return nil, err
		}
		if d.Confidence, err = r.f32(); err != nil {
			return nil, err
		}
		cid, err := r.u32()
		if err != nil {
			return nil, err
		}
		d.ClassID = int32(cid)
		resp.Detections = append(resp.Detections, d)
	}

	hasPrompt, err := r.u8()
	if err != nil {
		return nil, err
	}
	if hasPrompt == 1 {
		pb, err := r.box()
		if err != nil {
			return nil, err
		}
		resp.PromptBox = &pb
	}

	if resp.AnswerFeatures, err = r.features(); err != nil {
		return nil, err
	}
	if resp.PromptFeatures, err = r.features(); err != nil {
		return nil, err
	}

	if resp.Perf.PreprocessMs, err = r.i64(); err != nil {
		return nil, err
	}
	if resp.Perf.YoloInferMs, err = r.i64(); err != nil {
		return nil, err
	}
	if resp.Perf.CropResizeMs, err = r.i64(); err != nil {
		return nil, err
	}
	if resp.Perf.SiameseInferMs, err = r.i64(); err != nil {
		return nil, err
	}
	if resp.Perf.TotalMs, err = r.i64(); err != nil {
		return nil, err
	}

	if r.off != len(b) {
		return nil, fmt.Errorf("wire: %d trailing bytes after decode", len(b)-r.off)
	}
	return resp, nil
}
