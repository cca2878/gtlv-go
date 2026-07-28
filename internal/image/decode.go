// Package image 提供图像解码功能（本库内部使用）。
package image

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

// Decode 解码 JPEG/PNG 图像字节，返回 image.Image。
func Decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image decode failed: %w", err)
	}
	return img, nil
}

// Validate 校验字节是可解码的 JPEG/PNG，但只读文件头、不还原像素。
//
// 求解路径上真正解码是 wasm 里做的，这里只为在边界处给出类型化错误；
// 用 Decode 会为一张随即丢弃的位图付出整幅解码的代价。
func Validate(data []byte) error {
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("image decode failed: %w", err)
	}
	return nil
}
