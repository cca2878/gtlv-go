package solver

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// wasmBackend 持有 wazero 运行时与已实例化、已加载模型的 wasm 模块。
//
// wasm 模块单实例、guest 单线程，且求解经一段静态共享缓冲通信（写图→gt_solve→读结果），
// 故 infer 用 mu 串行化（验证码低频，一把锁足够）。
type wasmBackend struct {
	mu      sync.Mutex
	rt      wazero.Runtime
	mod     api.Module
	cache   wazero.CompilationCache
	gtSolve api.Function
	bufPtr  uint32
	bufCap  uint32
}

// compilationCache 返回持久化的 wazero 编译缓存（目录定位与降级见 resolveCacheDir）。
//
// wazero 首次把 wasm AOT 编成机器码需数秒；结果写入该目录后，二次及以后启动直接命中
// （首启冷、之后暖）。传入持久目录（WithCacheDir）可跨进程复用，是消除冷启的推荐做法。
func compilationCache(cacheDir string) (wazero.CompilationCache, error) {
	dir := resolveCacheDir(cacheDir)
	if dir == "" {
		return nil, fmt.Errorf("no writable cache dir")
	}
	return wazero.NewCompilationCacheWithDir(dir)
}

// newWasmBackend 实例化 wasm 模块、挂载模型目录、调用 gt_init 一次性加载模型（热态常驻）。
func newWasmBackend(ctx context.Context, o options) (*wasmBackend, error) {
	wasmMod, err := embeddedWasm()
	if err != nil {
		return nil, err
	}
	if o.wasmPath != "" {
		b, rerr := os.ReadFile(o.wasmPath)
		if rerr != nil {
			return nil, fmt.Errorf("read wasm %s: %w", o.wasmPath, rerr)
		}
		wasmMod = b
	}
	if len(wasmMod) == 0 {
		return nil, fmt.Errorf("no wasm module: embedded is empty and WithWasmPath not set")
	}

	rtConfig := wazero.NewRuntimeConfigCompiler()
	cache, cerr := compilationCache(o.cacheDir)
	if cerr == nil {
		rtConfig = rtConfig.WithCompilationCache(cache)
	}
	rt := wazero.NewRuntimeWithConfig(ctx, rtConfig)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	// 无需挂载任何文件系统：模型已由 gtlv-core 经 include_bytes! 编入 wasm 模块自身，
	// wasm 侧从内存字节加载（零外部文件、零临时目录）。
	// WithSysNanotime/Walltime：让 wasm 内 std::time::Instant 走真实时钟（否则默认固定时钟，
	// 内部 PerfTimer 全为 0）。
	modConfig := wazero.NewModuleConfig().
		WithStderr(os.Stderr).
		WithSysNanotime().
		WithSysWalltime()

	mod, err := rt.InstantiateWithConfig(ctx, wasmMod, modConfig)
	if err != nil {
		_ = rt.Close(ctx)
		if cache != nil {
			_ = cache.Close(ctx)
		}
		return nil, fmt.Errorf("instantiate wasm module: %w", err)
	}

	b := &wasmBackend{rt: rt, mod: mod, cache: cache, gtSolve: mod.ExportedFunction("gt_solve")}
	initFn := mod.ExportedFunction("gt_init")
	ptrFn := mod.ExportedFunction("gt_buffer_ptr")
	capFn := mod.ExportedFunction("gt_buffer_cap")
	if b.gtSolve == nil || initFn == nil || ptrFn == nil || capFn == nil {
		_ = b.close()
		return nil, fmt.Errorf("wasm module missing required exports (gt_init/gt_solve/gt_buffer_ptr/gt_buffer_cap)")
	}

	initRes, err := initFn.Call(ctx)
	if err != nil {
		_ = b.close()
		return nil, fmt.Errorf("gt_init call: %w", err)
	}
	if code := api.DecodeI32(initRes[0]); code != 0 {
		_ = b.close()
		return nil, fmt.Errorf("gt_init failed (code %d); model FS must contain yolo26n_gt_v2_384.onnx and siamese_feature.nnef.tgz", code)
	}

	ptrRes, _ := ptrFn.Call(ctx)
	capRes, _ := capFn.Call(ctx)
	b.bufPtr = uint32(ptrRes[0])
	b.bufCap = uint32(capRes[0])
	return b, nil
}

// close 关闭 wazero 运行时（释放模块与已加载模型）及编译缓存句柄。
func (b *wasmBackend) close() error {
	ctx := context.Background()
	err := b.rt.Close(ctx)
	if b.cache != nil {
		_ = b.cache.Close(ctx)
	}
	return err
}

// infer 求解：写图像到共享缓冲 → gt_solve → 从缓冲读回 wire 二进制 → 解码。
func (b *wasmBackend) infer(ctx context.Context, image []byte, conf float32) (*wireResponse, error) {
	if len(image) == 0 {
		return nil, fmt.Errorf("empty image")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if uint32(len(image)) > b.bufCap {
		return nil, fmt.Errorf("image %d bytes exceeds wasm buffer cap %d", len(image), b.bufCap)
	}
	if !b.mod.Memory().Write(b.bufPtr, image) {
		return nil, fmt.Errorf("write image to wasm memory failed")
	}

	res, err := b.gtSolve.Call(ctx, uint64(len(image)), api.EncodeF32(conf))
	if err != nil {
		return nil, fmt.Errorf("gt_solve call: %w", err)
	}
	n := api.DecodeI32(res[0])
	if n <= 0 {
		return nil, fmt.Errorf("gt_solve failed (code %d)", n)
	}

	// gt_solve 把 wire 结果写回同一缓冲首部；拷出后再解码（脱离 wasm 内存视图）。
	raw, ok := b.mod.Memory().Read(b.bufPtr, uint32(n))
	if !ok {
		return nil, fmt.Errorf("read wire result from wasm memory failed")
	}
	buf := make([]byte, n)
	copy(buf, raw)
	return decodeWireResponse(buf)
}
