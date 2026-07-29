# Makefile for gtlv-go（纯 WASM，无 CGO）

GO := go
CARGO := cargo
ZSTD := zstd
WASM_CRATE := rust-wasm
# 纯核 gtlv-core 现为独立仓（../gtlv-core），自带 fmt/clippy；本仓只 lint 自己的 rust-wasm 壳。
WASM_TARGET := wasm32-wasip1
# 构建目录固定为仓内绝对路径并导出：下面的 remap 前缀要与实际路径逐字对应，
# 若外部环境已设 CARGO_TARGET_DIR，产物就会落到别处、remap 失配。
CARGO_TARGET_DIR := $(CURDIR)/$(WASM_CRATE)/target
export CARGO_TARGET_DIR
WASM_OUT := $(CARGO_TARGET_DIR)/$(WASM_TARGET)/release/captcha_wasm.wasm
WASM_SRCS := $(wildcard $(WASM_CRATE)/src/*.rs)
# WASM_ZST：go:embed 携带的压缩 wasm（跨平台一份，随仓提交）
WASM_ZST := pkg/solver/captcha_wasm.wasm.zst
BIN := bin
BUILD := build

# 可复现构建：rustc 会把源码/依赖/OUT_DIR 的绝对路径写进产物（panic 位置等），
# 不消除则换台机器重编就得到不同字节，无从校验入库产物是否与源码一致。
# 三条 remap 覆盖全部三类路径来源；实测消除后跨机器 bit-identical。
# ⚠️ RUSTFLAGS 会整体覆盖 .cargo/config.toml 的 target.*.rustflags，
#    故 simd128 必须一并列在这里，否则会静默丢掉 tract 的手写 wasm 内核。
CARGO_HOME_DIR := $(if $(CARGO_HOME),$(CARGO_HOME),$(HOME)/.cargo)
WASM_RUSTFLAGS := -C target-feature=+simd128 \
  --remap-path-prefix=$(CURDIR)/$(WASM_CRATE)=/src \
  --remap-path-prefix=$(CARGO_HOME_DIR)=/cargo \
  --remap-path-prefix=$(CARGO_TARGET_DIR)=/target

.PHONY: all build-wasm build-go build-cli build-all test fmt check check-boundary check-tools clean deps help verify-wasm

all: build-all

deps:
	$(GO) mod tidy

# 外部构建工具检查。Rust 版本与 wasm target 由 rust-toolchain.toml 声明、rustup 按需安装；
# cargo 与 zstd 须预先安装，缺失时在此给出安装方式，而非由 shell 抛出 "not found"。
#
# 采用 zstd CLI 而非 Go 侧的压缩库：wasm.zst 随仓提交并 go:embed 进每个二进制与 aar，
# 而 klauspost/compress 最高档实测比 `zstd -19` 大 1.83%（约 515KB），该体积由全部下游承担。
check-tools:
	@ok=1; \
	command -v $(CARGO) >/dev/null 2>&1 || { \
	  echo "缺少 cargo：安装 Rust 工具链 https://rustup.rs"; ok=0; }; \
	command -v $(ZSTD) >/dev/null 2>&1 || { \
	  echo "缺少 zstd（用于压缩 go:embed 携带的 wasm）："; \
	  echo "    Debian/Ubuntu  sudo apt install zstd"; \
	  echo "    macOS          brew install zstd"; \
	  echo "    Windows        scoop install zstd"; ok=0; }; \
	[ $$ok = 1 ] || { echo "构建依赖缺失，见上。"; exit 1; }

# 编译 Rust → wasm32-wasip1（SIMD128 与 remap 见 WASM_RUSTFLAGS），压成 go:embed 用的 wasm.zst。
# 写成文件规则而非 .PHONY：源码没动时 zstd 那 13 秒不必反复付，也不会无谓重写入库产物。
build-wasm: $(WASM_ZST)

$(WASM_OUT): $(WASM_SRCS) $(WASM_CRATE)/Cargo.toml $(WASM_CRATE)/Cargo.lock \
             $(WASM_CRATE)/.cargo/config.toml rust-toolchain.toml | check-tools
	cd $(WASM_CRATE) && RUSTFLAGS='$(WASM_RUSTFLAGS)' $(CARGO) build --release --locked --target $(WASM_TARGET)

$(WASM_ZST): $(WASM_OUT)
	$(ZSTD) -19 -q -f -c $(WASM_OUT) > $@
	@echo "wasm.zst: $$(stat -c%s $@) bytes -> $@"

# 校验入库的 wasm.zst 确实由当前源码编出——构建可复现，所以字节比对就是有效判据。
# 比对解压后的 wasm 而非 .zst：zstd 不承诺跨版本比特一致，压缩器版本不该左右结论。
verify-wasm: $(WASM_OUT)
	@mkdir -p $(BUILD)
	@$(ZSTD) -d -q -f -o $(BUILD)/committed.wasm $(WASM_ZST)
	@if cmp -s $(BUILD)/committed.wasm $(WASM_OUT); then \
	  echo "入库 wasm 与源码一致 ✓"; \
	else \
	  echo "入库的 $(WASM_ZST) 与当前源码重编的结果不一致。"; \
	  echo "改过 rust-wasm 或升过 gtlv-core 后，须 make build-wasm 并把产物一并提交。"; \
	  exit 1; \
	fi

build-go: build-wasm
	$(GO) build ./...

build-cli: build-wasm
	@mkdir -p $(BIN)
	$(GO) build -o $(BIN)/gt-captcha-test ./cmd/gt-captcha-test/

build-all: build-cli

# Go 单测（含 wire 往返）+ Rust 单测（wire 编码，host 目标）
test: build-wasm
	$(GO) test ./...
	cd $(WASM_CRATE) && $(CARGO) test --release --locked

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
	@echo ">> cargo clippy -D warnings"; cd $(WASM_CRATE) && $(CARGO) clippy --release --locked -- -D warnings
	@echo ">> cargo test"; cd $(WASM_CRATE) && $(CARGO) test --release --locked
	@echo ">> verify-wasm"; $(MAKE) --no-print-directory verify-wasm
	@if command -v golangci-lint >/dev/null 2>&1; then echo ">> golangci-lint"; golangci-lint run; else echo ">> golangci-lint 未安装，跳过（CI 会跑）"; fi
	@echo "本地 CI 检查全绿 ✓"

# 边界守卫：纯本地层不得触网（net/http 只允许出现在 pkg/client）。
check-boundary:
	@if grep -rl '"net/http"' pkg/solver pkg/crypto internal/image internal/matcher internal/perf 2>/dev/null; then \
	  echo "边界被破坏：上述纯本地包引用了 net/http（网络只允许在 pkg/client）"; exit 1; \
	fi
	@echo "boundary OK: 仅 pkg/client 触网"

clean:
	rm -rf $(BIN) $(BUILD)
	cd $(WASM_CRATE) && $(CARGO) clean

help:
	@echo "  build-all           编 Rust→wasm→压缩内嵌→编 CLI"
	@echo "  verify-wasm         校验入库的 wasm.zst 与当前源码一致（CI 同款判据）"
	@echo "  check               推送前本地跑齐 CI 全部检查（与 CI 同工具链）"
	@echo "  check-tools         检查外部构建依赖是否就位"
	@echo "  test | fmt | clean | check-boundary"
	@echo ""
	@echo "构建依赖（编 wasm 时需要，go build/test 不需要）："
	@echo "  cargo  Rust 工具链，https://rustup.rs（版本与 wasm target 由 rust-toolchain.toml 声明）"
	@echo "  zstd   压缩 go:embed 携带的 wasm，apt/brew/scoop install zstd"
	@echo ""
	@echo "冷启：wazero 首次 AOT 编译 wasm 需数秒；用 solver.WithCacheDir 指定持久目录，"
	@echo "      结果写盘后二次及以后启动即暖（首启冷、之后暖）。"
