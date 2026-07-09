package solver

import (
	"encoding/binary"
	"math"
	"testing"
)

// buildWire 按 wire.rs 布局手工编码一个响应，供解码测试用（独立于生产解码器）。
func buildWire(dets []wireDetection, prompt *wireBox, ans, pr [][]float32, perf wirePerf) []byte {
	var b []byte
	putU32 := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }
	putF32 := func(v float32) { putU32(math.Float32bits(v)) }
	putI64 := func(v int64) { b = binary.LittleEndian.AppendUint64(b, uint64(v)) }
	putBox := func(x wireBox) { putF32(x.XMin); putF32(x.YMin); putF32(x.XMax); putF32(x.YMax) }
	putFeats := func(fs [][]float32) {
		putU32(uint32(len(fs)))
		for _, f := range fs {
			putU32(uint32(len(f)))
			for _, x := range f {
				putF32(x)
			}
		}
	}

	b = append(b, wireMagic[:]...)
	putU32(uint32(len(dets)))
	for _, d := range dets {
		putBox(d.Box)
		putF32(d.Confidence)
		putU32(uint32(d.ClassID))
	}
	if prompt != nil {
		b = append(b, 1)
		putBox(*prompt)
	} else {
		b = append(b, 0)
	}
	putFeats(ans)
	putFeats(pr)
	putI64(perf.PreprocessMs)
	putI64(perf.YoloInferMs)
	putI64(perf.CropResizeMs)
	putI64(perf.SiameseInferMs)
	putI64(perf.TotalMs)
	return b
}

func TestDecodeWireRoundTrip(t *testing.T) {
	dets := []wireDetection{
		{Box: wireBox{1, 2, 3, 4}, Confidence: 0.95, ClassID: 0},
		{Box: wireBox{5, 6, 7, 8}, Confidence: 0.5, ClassID: 0},
	}
	prompt := &wireBox{10, 11, 12, 13}
	ans := [][]float32{{0.1, -0.2, 0.3}, {1, 2, 3}}
	pr := [][]float32{{0.5}, {-0.5}}
	perf := wirePerf{PreprocessMs: 1, YoloInferMs: 2, CropResizeMs: 3, SiameseInferMs: 4, TotalMs: 583}

	resp, err := decodeWireResponse(buildWire(dets, prompt, ans, pr, perf))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(resp.Detections) != 2 || resp.Detections[1].Box != (wireBox{5, 6, 7, 8}) || resp.Detections[0].Confidence != 0.95 {
		t.Fatalf("detections mismatch: %+v", resp.Detections)
	}
	if resp.PromptBox == nil || *resp.PromptBox != *prompt {
		t.Fatalf("prompt box mismatch: %+v", resp.PromptBox)
	}
	if len(resp.AnswerFeatures) != 2 || resp.AnswerFeatures[0][1] != -0.2 || len(resp.PromptFeatures[0]) != 1 {
		t.Fatalf("features mismatch: %+v / %+v", resp.AnswerFeatures, resp.PromptFeatures)
	}
	if resp.Perf.TotalMs != 583 {
		t.Fatalf("perf mismatch: %+v", resp.Perf)
	}
}

func TestDecodeWireNoPromptBox(t *testing.T) {
	resp, err := decodeWireResponse(buildWire(nil, nil, nil, nil, wirePerf{}))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp.PromptBox != nil || len(resp.Detections) != 0 {
		t.Fatalf("expected empty response, got %+v", resp)
	}
}

func TestDecodeWireBadMagic(t *testing.T) {
	good := buildWire(nil, nil, nil, nil, wirePerf{})
	good[0] = 'X'
	if _, err := decodeWireResponse(good); err == nil {
		t.Fatal("expected bad-magic error")
	}
}

func TestDecodeWireShortBuffer(t *testing.T) {
	good := buildWire([]wireDetection{{Box: wireBox{1, 2, 3, 4}, ClassID: 0}}, nil, nil, nil, wirePerf{})
	if _, err := decodeWireResponse(good[:len(good)-3]); err == nil {
		t.Fatal("expected short-buffer error")
	}
	if _, err := decodeWireResponse(nil); err == nil {
		t.Fatal("expected error on empty buffer")
	}
}

func TestDecodeWireTrailingBytes(t *testing.T) {
	good := buildWire(nil, nil, nil, nil, wirePerf{})
	if _, err := decodeWireResponse(append(good, 0xFF)); err == nil {
		t.Fatal("expected trailing-bytes error")
	}
}
