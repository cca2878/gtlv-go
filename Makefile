# Makefile for gtlv-go（纯 WASM，无 CGO）

GO := go
CARGO := cargo
ZSTD := zstd
WASM_CRATE := rust-wasm
# 纯核 gtlv-core 现为独立仓（../gtlv-core），自带 fmt/clippy；本仓只 lint 自己的 rust-wasm 壳。
WASM_TARGET := wasm32-wasip1
WASM_OUT := $(WASM_CRATE)/target/$(WASM_TARGET)/release/captcha_wasm.wasm
# WASM_ZST：go:embed 携带的压缩 wasm（跨平台一份，随仓提交）
WASM_ZST := pkg/solver/captcha_wasm.wasm.zst
BIN := bin

.PHONY: all build-wasm build-go build-cli build-all test fmt check check-boundary clean deps help

all: build-all

deps:
	$(GO) mod tidy

# 编译 Rust → wasm32-wasip1（SIMD128 见 rust-wasm/.cargo/config.toml），压成 go:embed 用的 wasm.zst。
# 前置一次：rustup target add wasm32-wasip1
build-wasm:
	cd $(WASM_CRATE) && $(CARGO) build --release --target $(WASM_TARGET)
	$(ZSTD) -19 -q -f -c $(WASM_OUT) > $(WASM_ZST)
	@echo "wasm.zst: $$(stat -c%s $(WASM_ZST)) bytes -> $(WASM_ZST)"

build-go: build-wasm
	$(GO) build ./...

build-cli: build-wasm
	@mkdir -p $(BIN)
	$(GO) build -o $(BIN)/gt-captcha-test ./cmd/gt-captcha-test/

build-all: build-cli

# Go 单测（含 wire 往返）+ Rust 单测（wire 编码，host 目标）
test: build-wasm
	$(GO) test ./...
	cd $(WASM_CRATE) && $(CARGO) test --release

fmt:
	$(GO) fmt ./...
	$(CARGO) fmt --manifest-path $(WASM_CRATE)/Cargo.toml

# 推送前本地跑齐 CI 全部检查，避免"本地过、CI 挂"。Rust 侧用 rust-toolchain.toml
# 固定的版本，与 CI 完全一致。golangci-lint 未装则跳过（CI 仍会跑）。
check: check-boundary
	@echo ">> gofmt"
	@bad=$$(gofmt -l .); if [ -n "$$bad" ]; then echo "未 gofmt：$$bad"; exit 1; fi
	@echo ">> go vet"; $(GO) vet ./...
	@echo ">> go mod verify"; $(GO) mod verify
	@echo ">> go test -race"; $(GO) test -race ./...
	@echo ">> cargo fmt --check"; cd $(WASM_CRATE) && $(CARGO) fmt --check
	@echo ">> cargo clippy -D warnings"; cd $(WASM_CRATE) && $(CARGO) clippy --release -- -D warnings
	@echo ">> cargo test"; cd $(WASM_CRATE) && $(CARGO) test --release
	@if command -v golangci-lint >/dev/null 2>&1; then echo ">> golangci-lint"; golangci-lint run; else echo ">> golangci-lint 未安装，跳过（CI 会跑）"; fi
	@echo "本地 CI 检查全绿 ✓"

# 边界守卫：纯本地层不得触网（net/http 只允许出现在 pkg/client）。
check-boundary:
	@if grep -rl '"net/http"' pkg/solver pkg/crypto internal/image internal/matcher internal/perf 2>/dev/null; then \
	  echo "边界被破坏：上述纯本地包引用了 net/http（网络只允许在 pkg/client）"; exit 1; \
	fi
	@echo "boundary OK: 仅 pkg/client 触网"

clean:
	rm -rf $(BIN) build
	cd $(WASM_CRATE) && $(CARGO) clean

help:
	@echo "  build-all           编 Rust→wasm→压缩内嵌→编 CLI"
	@echo "  check               推送前本地跑齐 CI 全部检查（与 CI 同工具链）"
	@echo "  test | fmt | clean | check-boundary"
	@echo "首次需: rustup target add wasm32-wasip1"
	@echo ""
	@echo "冷启：wazero 首次 AOT 编译 wasm 需数秒；用 solver.WithCacheDir 指定持久目录，"
	@echo "      结果写盘后二次及以后启动即暖（首启冷、之后暖）。"
