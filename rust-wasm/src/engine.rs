//! 推理编排：YOLO 检测 → NMS/Top-K 过滤 → 裁剪 + Siamese 特征提取。
//!
//! 纯逻辑，产物为 `wire::DetectResult`（进程内 FFI 结果，序列化见 wire.rs）。
use anyhow::Result;
use std::path::Path;

use crate::perf::PerfTimer;
use crate::siamese::SiameseExtractor;
use crate::wire::{BoundingBox, DetectResult, Detection, PerfTimings};
use crate::yolo::YoloDetector;

/// 承载已加载模型的推理引擎（一次加载，多次求解；无内部可变状态，可并发调用）。
pub struct Engine {
    yolo: YoloDetector,
    siamese: SiameseExtractor,
}

impl Engine {
    /// nano yolo 输入尺寸（选定规格 nano@384）。
    const YOLO_INPUT: usize = 384;

    pub fn new(yolo_path: &Path, siamese_path: &Path) -> Result<Self> {
        // 顺序加载：wasm guest 单线程，并行加载（thread::scope）是原生专利，此处用不上。
        let yolo = YoloDetector::new(yolo_path, Self::YOLO_INPUT)?;
        let siamese = SiameseExtractor::new(siamese_path)?;
        Ok(Self { yolo, siamese })
    }

    /// 检测 + 特征提取，返回 `wire::DetectResult`。`timer` 由调用方提供（FFI 侧传禁用计时器）。
    pub fn detect_and_extract(
        &self,
        image_bytes: &[u8],
        conf_threshold: f32,
        timer: &PerfTimer,
    ) -> Result<DetectResult> {
        let total_start = std::time::Instant::now();

        // 1. 图像预处理 + YOLO 检测
        timer.start("preprocess");
        timer.stop("preprocess");

        timer.start("yolo_infer");
        let conf_threshold = if conf_threshold > 0.0 {
            conf_threshold
        } else {
            0.5
        };
        let detections = self.yolo.detect(image_bytes, conf_threshold)?;
        timer.stop("yolo_infer");

        // 2. class 0 后处理：NMS 去重 + Top-K 兜底（与 Python filter_ans_boxes 对齐）
        let mut ans_boxes: Vec<_> = detections
            .iter()
            .filter(|d| d.class_id == 0)
            .cloned()
            .collect();
        YoloDetector::nms(&mut ans_boxes, 0.5);
        if ans_boxes.len() > 4 {
            ans_boxes.sort_by(|a, b| b.confidence.partial_cmp(&a.confidence).unwrap());
            ans_boxes.truncate(4);
        }

        // class 1：取置信度最高的提示词整体框
        let prompt_box = detections
            .iter()
            .filter(|d| d.class_id == 1)
            .max_by(|a, b| a.confidence.partial_cmp(&b.confidence).unwrap())
            .cloned();

        // 3. 裁剪 + 特征提取
        timer.start("crop_resize");
        timer.start("siamese_infer");
        let img = image::load_from_memory(image_bytes)?;

        let mut out_detections: Vec<Detection> = Vec::new();
        let mut answer_features: Vec<Vec<f32>> = Vec::new();
        let mut prompt_features: Vec<Vec<f32>> = Vec::new();

        for (di, det) in ans_boxes.iter().enumerate() {
            match self
                .siamese
                .extract_features_from_crop(&img, det.x_min, det.y_min, det.x_max, det.y_max)
            {
                Ok(features) => {
                    out_detections.push(Detection {
                        bbox: BoundingBox {
                            x_min: det.x_min,
                            y_min: det.y_min,
                            x_max: det.x_max,
                            y_max: det.y_max,
                        },
                        confidence: det.confidence,
                        class_id: det.class_id,
                    });
                    answer_features.push(features);
                }
                Err(e) => {
                    log::warn!("Feature extraction failed for ans box {}: {}", di, e);
                    continue;
                }
            }
        }

        let out_prompt_box = if let Some(ref pb) = prompt_box {
            let n_answers = ans_boxes.len().max(1);
            let seg_width = (pb.x_max - pb.x_min) / n_answers as f32;
            for i in 0..n_answers {
                let x_min = pb.x_min + i as f32 * seg_width;
                let x_max = x_min + seg_width;
                match self
                    .siamese
                    .extract_features_from_crop(&img, x_min, pb.y_min, x_max, pb.y_max)
                {
                    Ok(features) => prompt_features.push(features),
                    Err(e) => {
                        log::warn!("Feature extraction failed for prompt segment {}: {}", i, e);
                        continue;
                    }
                }
            }
            Some(BoundingBox {
                x_min: pb.x_min,
                y_min: pb.y_min,
                x_max: pb.x_max,
                y_max: pb.y_max,
            })
        } else {
            None
        };

        timer.stop("siamese_infer");
        timer.stop("crop_resize");

        Ok(DetectResult {
            detections: out_detections,
            answer_features,
            prompt_features,
            prompt_box: out_prompt_box,
            perf: PerfTimings {
                preprocess_ms: timer.elapsed_ms("preprocess"),
                yolo_infer_ms: timer.elapsed_ms("yolo_infer"),
                crop_resize_ms: timer.elapsed_ms("crop_resize"),
                siamese_infer_ms: timer.elapsed_ms("siamese_infer"),
                total_ms: total_start.elapsed().as_millis() as i64,
            },
        })
    }
}
