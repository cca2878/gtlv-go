# gtlv-go · gt 验证码求解库（纯 WASM，无 CGO）

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](./LICENSE)

gt 验证码求解库。点选推理经 [wazero](https://github.com/tetratelabs/wazero) **进程内加载** Rust 推理模块（[`tract`](https://github.com/sonos/tract) 编为 `wasm32-wasip1`）完成——**全程无 CGO、无子进程、无 `.so` 分发**，一份 `go:embed` 的 `.wasm` 通吃所有支持平台。

```
import "github.com/cca2878/gtlv-go/pkg/solver"
```

- **无 CGO**：Go 侧 100% 纯 Go（wazero + klauspost/compress），Rust 侧一次编成 `.wasm` 各平台通用。免去 NDK / `.so` 打包 / 16KB 对齐 / ABI 矩阵等原生集成负担。
- **单二进制**：wasm 经 `go:embed` 压缩内嵌，`go get` 即用。
- **首启冷、之后暖**：wazero 首次 AOT 编译 wasm 需数秒；用 `solver.WithCacheDir` 指定持久目录，结果写盘后二次及以后启动即命中（见 [编译缓存](#编译缓存)）。

## 架构：本地层与网络层界限分明

本库刻意把**纯本地推理**与**依赖网络的协议编排**分成两层，边界清晰、各自可独立维护/替换：

```
┌─ 网络外层（唯一触网）────────────────────────────────────────┐
│  pkg/client   gt V3 协议编排：challenge → 拉图 → 提交 → validate │
└───────────────┬──────────────────────────────────────────────┘
                │ 仅通过 solver.Solve / crypto 调用下层
┌─ 纯本地层（离线可测，不触网）────────────────────────────────┐
│  pkg/solver          点选求解管线：图像 → 检测/匹配 → 坐标        │
│    └ wazero 后端 + go:embed wasm（无 CGO）                        │
│  pkg/crypto          w 参数生成（点选/滑动）                       │
│  pkg/solver/classic  滑动求解：还原 → 缺口识别 → 轨迹              │
│  internal/{image,matcher,perf}  图像解码 / 匈牙利匹配 / 计时       │
└──────────────────────────────────────────────────────────────┘
```

**边界不变量**：整个库中**只有 `pkg/client` 引用 `net/http`**。纯本地层可完全离线使用（自备图像字节即可求解），网络层是薄而可替换的编排器。最终目标形态：`输入 challenge 等信息 → 输出 validate`，由网络层内部调用本地层完成。

## 安装

```bash
go get github.com/cca2878/gtlv-go
```

`go get` 已包含内嵌的 `.wasm`；点选推理还需两个模型文件（见 [模型](#模型)），经 `WithModelDir` 指向其目录。

## 使用

### 纯本地：图像 → 坐标 → W 参数（离线）

```go
import (
    "context"
    "github.com/cca2878/gtlv-go/pkg/solver"
    "github.com/cca2878/gtlv-go/pkg/crypto"
)

s, err := solver.NewCaptchaSolver(
    solver.WithModelDir("./models"), // 含 yolo26n_gt_v2_384.onnx 与 siamese_feature.nnef.tgz
    // solver.WithCacheDir("/path"), // 可选；移动端须显式注入 app 私有可写目录
    // solver.WithWasmPath("x.wasm"),// 可选；默认用内嵌模块
)
if err != nil { panic(err) }
defer s.Close()

res, err := s.Solve(context.Background(), imageBytes) // JPEG/PNG
if err != nil { panic(err) }

points := make([][2]float64, len(res.Matches))
for i, m := range res.Matches { points[i] = [2]float64{m.X, m.Y} }
w, err := crypto.ClickCalculate(points, gt, challenge) // → W 参数
```

### 网络层：challenge → validate（见[状态](#状态)）

`V3Client` 可复用（配置一次、并发安全），按类型自动分派点选/滑动，失败可换图重试：

```go
import (
    "errors"
    "github.com/cca2878/gtlv-go/pkg/client"
)

// 建一次，反复用。点选需传入本地求解器 s；滑动传 nil 也可。
c := client.NewV3Client(client.WithMaxAttempts(3)) // 失败最多试 3 次
// 亦可：WithHTTPClient(...) / WithHosts(...) / WithVerifyDelay(...)

validate, err := c.GetValidate(context.Background(), gt, challenge, s)
switch {
case err == nil:
    // 用 validate
case errors.Is(err, client.ErrVerificationFailed):
    // 重试后仍未通过（可取 *client.VerifyError 看服务端原因）
default:
    // 网络/配置等错误
}
```

gt/challenge 由你的业务接口提供；`client.Register` 可从公开登记端点取一对用于自测。端到端联网冒烟：

```bash
go run ./cmd/gt-captcha-e2e -models ./models -n 3   # 登记→拉图→求解→提交→打印 validate
```

## 支持平台

wazero 的 JIT 编译器只有 **amd64 / arm64** 后端，故仅这两个架构受支持（其余架构编译期即报错）：

| 架构 | Linux | Windows | Android | macOS |
|---|:---:|:---:|:---:|:---:|
| x86_64 (amd64) | ✅ | ✅ | ✅ | ✅ |
| ARMv8 (arm64) | ✅ | ✅ | ✅ | ✅ |
| ARMv7 (arm) | ❌ | — | ❌ | — |

ARMv7 无 wazero 编译器后端，**明确不支持**（`pkg/solver/unsupported.go` 在此类架构上编译期报错，避免误分发出启动即崩的二进制）。

## 构建

```bash
rustup target add wasm32-wasip1   # 首次
make build-all                    # 编 Rust→wasm→压缩内嵌→编 CLI

./bin/gt-captcha-test -image testdata/sample.png -gt <GT> -challenge <CHALLENGE> -verbose
```

### 编译缓存

wazero 首次把 wasm AOT 编成机器码需数秒（**首启冷**）。给 `NewCaptchaSolver` 传一个持久目录即可让二次及以后启动直接命中（**之后暖**）：

```go
solver.NewCaptchaSolver(
    solver.WithModelDir("./models"),
    solver.WithCacheDir("/var/lib/myapp/gtlv"), // 持久、可写；缺省用 os.UserCacheDir()/gtlv-go
)
```

缺省会落在 `os.UserCacheDir()/gtlv-go`（不可写则退到临时目录）。移动端等宿主应显式传入 App 私有可写目录。

> **为什么不把预烤缓存内嵌进库？** Go 无 wheel 式二进制发布——`go get` 只经 module proxy 拉取已提交的仓库源码，无法给版本附带独立构建产物。要让使用方免掉那一次冷启，只能把 ~10MB/架构的编译缓存提交进仓库，而那是会随 wazero 版本 churn 的 CI 产物，得不偿失。既然 `WithCacheDir` 已让「首启冷、之后暖」，本库便只内嵌 wasm，不内嵌编译缓存。

## 模型

点选需两个模型文件（放 `models/`，经 `WithModelDir` 指向）：

- `yolo26n_gt_v2_384.onnx` —— nano@384 目标检测（~9.7MB）
- `siamese_feature.nnef.tgz` —— 特征提取（~11MB）

选定规格 nano@384 在纯 CPU 上单次求解约 0.5–0.7s，图级准确率约 94%。模型默认**不内嵌**（作外部目录，避免二进制臃肿）；如需真·单文件，可自行 `go:embed` 模型并用 wazero `WithFSMount` 挂载。

## 目录

代码分区清晰，公开 API 面刻意收窄：**库主体 `pkg/`** 只暴露 solver / client / crypto / classic 四个包；实现细节都在 **`internal/`**（不可被外部 import）；`cmd/` 是示例/自测 CLI。

| 路径 | 说明 |
|---|---|
| `pkg/solver/` | 点选求解 API（`solver.go`，含 `Err*` 类型化错误）、wazero 后端（`wazero.go`）、缓存目录定位（`cachedir.go`）、内嵌 wasm（`embed.go`）、wire 解码（`wire.go`） |
| `pkg/solver/classic/` | 滑动求解（背景还原 + 缺口识别 + 轨迹；本地图像处理，`SolveSlide`；AGPL 来源见 [许可](#许可)） |
| `pkg/crypto/` | w 参数加密（点选/滑动），仅暴露 `ClickCalculate`/`SlideCalculate`（纯本地） |
| `pkg/client/` | gt V3 协议编排（**唯一触网层**）：`v3.go` 可复用客户端+分派，`click.go`/`slide.go` 两条路径，`register.go` 自测登记，`errors.go` 类型化错误 |
| `internal/{image,matcher,perf}/` | 图像解码、匈牙利匹配、计时（实现细节，不对使用方暴露） |
| `cmd/gt-captcha-test/` | 点选本地调试 CLI（给定图片/参数出坐标与 W） |
| `cmd/gt-captcha-e2e/` | 端到端联网冒烟（登记 → 自动分派点选/滑动 → validate） |
| `rust-wasm/` | Rust 推理核心 → `wasm32-wasip1`（`lib.rs` 导出 gt_init/gt_solve/gt_buffer_*） |
| `models/` | 点选模型（onnx + nnef.tgz），随库入仓 |

## 状态

- **点选（pkg/solver + pkg/client 点选路径）**：**已真机联网验证通过**——`cmd/gt-captcha-e2e` 走完整「登记 → 拉图 → wasm 求解 → 算 w → 提交」链路，对真实 gt 端点连续多轮取回有效 validate（端点/JSONP/坐标缩放对齐 [biliTicker_gt](https://github.com/Amorter/biliTicker_gt)）。
- **滑动（pkg/solver/classic + pkg/client 滑动路径）**：与点选对等的一等公民——`GetValidate` 按类型自动分派，滑动走纯本地「还原 → 缺口识别 → 轨迹」，用本轮新 challenge 与 c/s 算 w。协议编排与解析已离线单测；因公开自测端点（Bilibili）仅下发点选，滑动路径尚未真机联网跑通。
- **反机器时延**：`verify` 前的等待按「本轮签发 → 提交」总时长补足到 2s（非固定睡满），wasm 推理耗时自然计入。可经 `WithVerifyDelay` 调整。

### 工程化

- **可复用客户端**：`V3Client` 只承载配置、并发安全，`gt/challenge` 属单次调用；配置一次即可反复求解。
- **类型化错误**：`errors.Is(err, client.ErrVerificationFailed)` 判定「识别未通过」（可重试），`errors.As(&client.VerifyError{})` 取服务端原因；另有 `ErrSolverRequired`、`UnsupportedCaptchaTypeError`。求解层有 `solver.Err*` 一组哨兵。
- **可配置重试**：`WithMaxAttempts(n)`，仅对可重试失败换图重试。
- **CI/质量门**：`.github/workflows/ci.yml`（gofmt/vet/`test -race`/边界/golangci-lint + Rust 侧 clippy/test）；`.golangci.yml` 稳健 linter 集。

## 许可

本项目采用 **[AGPL-3.0](./LICENSE)**。

以下部分移植自 [Amorter](https://github.com/Amorter) 的 AGPL-3.0 项目，故本库整体采用 AGPL-3.0 以保持许可兼容：

- **w 参数加密 / 点选网络协议 / 滑块轨迹**（`pkg/crypto`、`pkg/client`、`pkg/solver/classic`）—— 移植自 [biliTicker_gt](https://github.com/Amorter/biliTicker_gt)（`src/w.rs`、`src/click.rs`、`src/abstraction.rs`、`src/slide.rs`）。
- **点选识别流程**（YOLO 检测 + Siamese 特征匹配的整体思路）—— 参考自 [CaptchaBreaker](https://github.com/Amorter/CaptchaBreaker)。
