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
