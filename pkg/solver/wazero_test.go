package solver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// emptyWasm 是最小的合法 wasm 模块（magic + version），够走完实例化里装配 WASI stdio 那一段，
// 又不必背上真模块那 20 余 MB 的编译开销。
var emptyWasm = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

// instantiateWithStderr 复刻 newWasmBackend 里 wazero 的装配方式，只保留与 stderr 有关的部分。
func instantiateWithStderr(t *testing.T, stderr io.Writer) error {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	defer func() { _ = rt.Close(ctx) }()
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)
	cfg := wazero.NewModuleConfig().WithStderr(stderr).WithSysNanotime().WithSysWalltime()
	_, err := rt.InstantiateWithConfig(ctx, emptyWasm, cfg)
	return err
}

// TestStderrWriterKeepsUsableStderr 确认可用的 stderr 被原样传下去，wasm 的 panic 文本不被吞掉。
func TestStderrWriterKeepsUsableStderr(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if got := stderrWriter(f); got != io.Writer(f) {
		t.Fatalf("可用的 stderr 应被原样返回，得到 %T", got)
	}
}

// TestStderrWriterFallsBackWhenUnusable 覆盖宿主没有标准错误流的两种形态。
// 无效句柄这一种正是 Windows GUI 子系统（-H=windowsgui）下 os.Stderr 的样子。
func TestStderrWriterFallsBackWhenUnusable(t *testing.T) {
	cases := map[string]*os.File{
		"nil":     nil,
		"坏的文件描述符": os.NewFile(uintptr(9999), "/dev/stderr"),
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			if got := stderrWriter(f); got != io.Discard {
				t.Fatalf("不可用的 stderr 应降级为 io.Discard，得到 %T", got)
			}
		})
	}
}

// TestInstantiateSurvivesUnusableStderr 是这条修复的回归判据：把不可用的 stderr 直接交给
// wazero 会让实例化失败，经 stderrWriter 之后必须成功。
func TestInstantiateSurvivesUnusableStderr(t *testing.T) {
	broken := os.NewFile(uintptr(9999), "/dev/stderr")

	// 先确认这确实是一个会炸的输入——否则本测试形同虚设。
	if err := instantiateWithStderr(t, broken); err == nil {
		t.Fatal("预期直接传入坏句柄会让实例化失败，却成功了；该用例已失去意义")
	}

	if err := instantiateWithStderr(t, stderrWriter(broken)); err != nil {
		t.Fatalf("经 stderrWriter 处理后实例化仍失败: %v", err)
	}
}
