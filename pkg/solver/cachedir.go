package solver

import (
	"os"
	"path/filepath"
)

// resolveCacheDir 定位 wazero 编译缓存目录：
//   - configured 非空 → 用它（移动端宿主必须走此路，注入 app 私有可写目录）；
//   - 否则默认 os.UserCacheDir()/gtlv-go；
//   - 建目录失败（只读根文件系统等）→ 降级到 os.TempDir()（每冷进程重编译，但不崩）。
//
// 返回空串表示连临时目录都不可写：调用方应放弃缓存、退回纯冷启。
//
// wazero 会把首次 AOT 编译结果写入该目录，二次启动即命中——即所谓「首启冷、之后暖」。
// 传入持久化目录（WithCacheDir）可跨进程复用。
func resolveCacheDir(configured string) string {
	candidates := make([]string, 0, 3)
	if configured != "" {
		candidates = append(candidates, configured)
	} else if base, err := os.UserCacheDir(); err == nil {
		candidates = append(candidates, filepath.Join(base, "gtlv-go"))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), "gtlv-go"))

	for _, dir := range candidates {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return dir
		}
	}
	return ""
}
