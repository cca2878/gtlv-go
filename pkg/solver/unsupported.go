//go:build !amd64 && !arm64

package solver

// wazero 的 JIT 编译器只有 amd64/arm64 两个后端（见 wazevo/isa_amd64.go、isa_arm64.go）。
// 其他架构（如 ARMv7 / GOARCH=arm）没有编译器后端，NewRuntimeConfigCompiler 会在运行时 panic。
// 本库【明确不支持】这些架构，在此于编译期即报错，避免误分发出「启动即崩」的二进制。
//
// 触发的编译错误形如：undefined: goGtCaptcha_unsupportedGOARCH_onlyAmd64AndArm64
const _ = goGtCaptcha_unsupportedGOARCH_onlyAmd64AndArm64
