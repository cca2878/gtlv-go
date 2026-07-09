//! FFI 线格式：进程内 Rust→Go 传递推理结果的极简小端二进制。
//!
//! 取代 gRPC 时代的 protobuf——进程内、同机、同架构、小端、无网络、无跨语言前向兼容
//! 需求，protobuf 的 varint/schema/兼容性全无用武之地。这里用一段定长小端布局，Rust 一次
//! 顺序写出、Go 用游标读入，零第三方依赖。布局须与 Go 侧 `pkg/solver/wire.go` 逐字节对应：
//!
//! **改动布局必须同步两侧并 bump MAGIC**（Go 侧校验魔数，不符即报错，防陈旧 .so/ABI 漂移）。
//!
//! ```text
//!   [4]u8 magic = "GTC1"
//!   u32  n_detections
//!   n_detections × { f32 x_min, y_min, x_max, y_max, confidence ; i32 class_id }   // 24B
//!   u8   has_prompt_box (0/1)
//!   [if 1] f32 x_min, y_min, x_max, y_max                                          // 16B
//!   u32  n_answer_features
//!   n_answer_features × { u32 dim ; dim × f32 }
//!   u32  n_prompt_features
//!   n_prompt_features × { u32 dim ; dim × f32 }
//!   i64  preprocess_ms, yolo_infer_ms, crop_resize_ms, siamese_infer_ms, total_ms  // 40B
//! ```

/// 线格式魔数/版本。改动布局须同步 bump（Go 侧同名常量）。
pub const MAGIC: [u8; 4] = *b"GTC1";

#[derive(Clone, Copy, Default)]
pub struct BoundingBox {
    pub x_min: f32,
    pub y_min: f32,
    pub x_max: f32,
    pub y_max: f32,
}

#[derive(Clone, Copy)]
pub struct Detection {
    pub bbox: BoundingBox,
    pub confidence: f32,
    pub class_id: i32,
}

#[derive(Clone, Copy, Default)]
pub struct PerfTimings {
    pub preprocess_ms: i64,
    pub yolo_infer_ms: i64,
    pub crop_resize_ms: i64,
    pub siamese_infer_ms: i64,
    pub total_ms: i64,
}

/// 推理结果（引擎产物）。answer_features 与 detections 一一对应；prompt_features 按位置排列。
#[derive(Default)]
pub struct DetectResult {
    pub detections: Vec<Detection>,
    pub prompt_box: Option<BoundingBox>,
    pub answer_features: Vec<Vec<f32>>,
    pub prompt_features: Vec<Vec<f32>>,
    pub perf: PerfTimings,
}

impl DetectResult {
    /// 序列化为小端字节缓冲（见模块文档布局）。
    pub fn encode(&self) -> Vec<u8> {
        let mut b = Vec::with_capacity(self.encoded_len_hint());

        b.extend_from_slice(&MAGIC);
        b.extend_from_slice(&(self.detections.len() as u32).to_le_bytes());
        for d in &self.detections {
            put_box(&mut b, &d.bbox);
            b.extend_from_slice(&d.confidence.to_le_bytes());
            b.extend_from_slice(&d.class_id.to_le_bytes());
        }

        match &self.prompt_box {
            Some(pb) => {
                b.push(1);
                put_box(&mut b, pb);
            }
            None => b.push(0),
        }

        put_features(&mut b, &self.answer_features);
        put_features(&mut b, &self.prompt_features);

        for v in [
            self.perf.preprocess_ms,
            self.perf.yolo_infer_ms,
            self.perf.crop_resize_ms,
            self.perf.siamese_infer_ms,
            self.perf.total_ms,
        ] {
            b.extend_from_slice(&v.to_le_bytes());
        }

        b
    }

    fn encoded_len_hint(&self) -> usize {
        let feat_bytes =
            |fs: &Vec<Vec<f32>>| -> usize { 4 + fs.iter().map(|f| 4 + f.len() * 4).sum::<usize>() };
        MAGIC.len()
            + 4
            + self.detections.len() * 24
            + 1
            + 16
            + feat_bytes(&self.answer_features)
            + feat_bytes(&self.prompt_features)
            + 40
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn encode_starts_with_magic_and_matches_hint() {
        let r = DetectResult {
            detections: vec![Detection {
                bbox: BoundingBox {
                    x_min: 1.0,
                    y_min: 2.0,
                    x_max: 3.0,
                    y_max: 4.0,
                },
                confidence: 0.9,
                class_id: 0,
            }],
            prompt_box: Some(BoundingBox {
                x_min: 5.0,
                y_min: 6.0,
                x_max: 7.0,
                y_max: 8.0,
            }),
            answer_features: vec![vec![0.5, -0.25]],
            prompt_features: vec![vec![1.0]],
            perf: PerfTimings {
                total_ms: 42,
                ..Default::default()
            },
        };
        let b = r.encode();
        assert_eq!(&b[..4], &MAGIC);
        // magic(4)+n_det(4)+det(24)+has_pb(1)+pb(16)+n_ans(4)+[dim(4)+2*4]+n_pr(4)+[dim(4)+1*4]+perf(40)
        assert_eq!(
            b.len(),
            4 + 4 + 24 + 1 + 16 + 4 + (4 + 8) + 4 + (4 + 4) + 40
        );
    }
}

fn put_box(b: &mut Vec<u8>, bb: &BoundingBox) {
    b.extend_from_slice(&bb.x_min.to_le_bytes());
    b.extend_from_slice(&bb.y_min.to_le_bytes());
    b.extend_from_slice(&bb.x_max.to_le_bytes());
    b.extend_from_slice(&bb.y_max.to_le_bytes());
}

fn put_features(b: &mut Vec<u8>, features: &[Vec<f32>]) {
    b.extend_from_slice(&(features.len() as u32).to_le_bytes());
    for f in features {
        b.extend_from_slice(&(f.len() as u32).to_le_bytes());
        for &x in f {
            b.extend_from_slice(&x.to_le_bytes());
        }
    }
}
