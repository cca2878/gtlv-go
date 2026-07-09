package solver

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// embeddedWasmZst 是压缩后的推理 wasm 模块（zstd）。构建期 `make build-wasm` 产出并压缩。
// 跨平台通用（同一份 .wasm 供所有目标），故无条件嵌入、运行时解压一次。
// 可用 WithWasmPath 覆盖为外部 .wasm 文件。
//
//go:embed captcha_wasm.wasm.zst
var embeddedWasmZst []byte

var (
	wasmOnce  sync.Once
	wasmBytes []byte
	wasmErr   error
)

// embeddedWasm 解压并缓存内嵌 wasm 字节（进程内一次）。
func embeddedWasm() ([]byte, error) {
	wasmOnce.Do(func() {
		wasmBytes, wasmErr = zstdDecode(embeddedWasmZst)
		if wasmErr != nil {
			wasmErr = fmt.Errorf("decompress embedded wasm: %w", wasmErr)
		}
	})
	return wasmBytes, wasmErr
}

// —— zstd 解码器（纯 Go，无 CGO）——
// 一个 *zstd.Decoder 可并发用于 DecodeAll，故进程内共享一个。
var (
	zstdDecOnce sync.Once
	zstdDec     *zstd.Decoder
	zstdDecErr  error
)

func zstdDecode(b []byte) ([]byte, error) {
	zstdDecOnce.Do(func() {
		zstdDec, zstdDecErr = zstd.NewReader(nil)
	})
	if zstdDecErr != nil {
		return nil, zstdDecErr
	}
	return zstdDec.DecodeAll(b, nil)
}
