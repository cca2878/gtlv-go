//! wasm32-wasip1 模块：供 Go 侧经 wazero 加载调用（纯 WASM，无 CGO）。
//!
//! 推理编排在 `engine::Engine`，结果经 `wire` 模块的极简小端二进制序列化跨边界传回 Go
//! （与原生 FFI 同一套 wire 格式，非 JSON）。
//!
//! ABI（静态共享缓冲，不跨边界传结构体/指针所有权）：
//!   - `gt_buffer_ptr() -> u32`        共享缓冲在 wasm 线性内存中的偏移
//!   - `gt_buffer_cap() -> u32`        缓冲容量（字节）
//!   - `gt_init() -> i32`              从固定 WASI 路径加载模型（一次）；0=ok，<0=err
//!   - `gt_solve(img_len, conf) -> i32`  读缓冲前 img_len 字节为图像 → 求解 → 把 wire 二进制写回缓冲首部，返回其长度(>0)；<0=err
//!
//! 并发：模块单实例、guest 单线程；Go 侧对每次「写图→gt_solve→读结果」加锁串行化。
mod engine;
mod perf;
mod siamese;
mod wire;
mod yolo;

use std::ptr::addr_of_mut;
use std::sync::OnceLock;

use engine::Engine;
use perf::PerfTimer;

const BUFFER_SIZE: usize = 4 * 1024 * 1024;
static mut SHARED_BUFFER: [u8; BUFFER_SIZE] = [0u8; BUFFER_SIZE];
static ENGINE: OnceLock<Engine> = OnceLock::new();

// 模型路径：Go 侧经 wazero `WithFSMount` 把模型目录挂到 `/models`。
const YOLO_PATH: &str = "/models/yolo26n_gt_v2_384.onnx";
const SIAMESE_PATH: &str = "/models/siamese_feature.nnef.tgz";

#[no_mangle]
pub extern "C" fn gt_buffer_ptr() -> u32 {
    addr_of_mut!(SHARED_BUFFER) as usize as u32
}

#[no_mangle]
pub extern "C" fn gt_buffer_cap() -> u32 {
    BUFFER_SIZE as u32
}

/// 加载模型（一次，热态常驻）。重复调用返回 -2。
#[no_mangle]
pub extern "C" fn gt_init() -> i32 {
    let yolo = std::path::Path::new(YOLO_PATH);
    let siamese = std::path::Path::new(SIAMESE_PATH);
    match Engine::new(yolo, siamese) {
        Ok(engine) => {
            if ENGINE.set(engine).is_err() {
                return -2;
            }
            0
        }
        Err(e) => {
            eprintln!("gt_init failed: {e}");
            -1
        }
    }
}

/// 求解：图像字节须已写入共享缓冲前 `img_len` 字节。
#[no_mangle]
pub extern "C" fn gt_solve(img_len: u32, conf: f32) -> i32 {
    let engine = match ENGINE.get() {
        Some(e) => e,
        None => return -1,
    };
    let img_len = img_len as usize;
    if img_len == 0 || img_len > BUFFER_SIZE {
        return -2;
    }
    // 先把图像拷出，释放对共享缓冲的借用（随后要把结果写回同一缓冲）。
    let img = unsafe {
        std::slice::from_raw_parts(addr_of_mut!(SHARED_BUFFER) as *const u8, img_len).to_vec()
    };

    let timer = PerfTimer::new(true);
    let result = match engine.detect_and_extract(&img, conf, &timer) {
        Ok(r) => r,
        Err(e) => {
            eprintln!("gt_solve failed: {e}");
            return -3;
        }
    };

    let encoded = result.encode();
    if encoded.len() > BUFFER_SIZE {
        return -4;
    }
    unsafe {
        let dst =
            std::slice::from_raw_parts_mut(addr_of_mut!(SHARED_BUFFER) as *mut u8, encoded.len());
        dst.copy_from_slice(&encoded);
    }
    encoded.len() as i32
}
