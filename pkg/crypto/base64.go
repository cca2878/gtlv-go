package crypto

// ── 自定义 Base64 ──────────────────────────────────────────────────
// gt 自定义 Base64 编码表（64 字符 + '.' 作为 padding）。
var base64Table = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789()")

// 位掩码，用于从 24-bit 输入中提取对应 base64 字符的 6-bit 索引。
// getIntByMask 从高位到低位扫描 mask 中为 1 的位，依次从 base 中提取对应位并打包。
// 每个 mask 恰好有 6 个为 1 的位，因此每个输出索引为 6 bit。
//
//	mask1 = 0x6F0000 → 提取 bits {22,21,19,18,17,16} → 第 1 个字符
//	mask2 = 0x90B400 → 提取 bits {23,19,15,13,12,10} → 第 2 个字符
//	mask3 = 0x4B14   → 提取 bits {14,11,9,8,5,3}     → 第 3 个字符
//	mask4 = 0xEB     → 提取 bits {7,6,5,3,1,0}       → 第 4 个字符
const (
	mask1 = 7274496 // 0x6F0000
	mask2 = 9483264 // 0x90B400
	mask3 = 19220   // 0x4B14
	mask4 = 235     // 0xEB
)

func chooseBit(base, bit int) int {
	return (base >> bit) & 1
}

func getIntByMask(base, mask int) int {
	res := 0
	for bit := 23; bit >= 0; bit-- {
		if chooseBit(mask, bit) == 1 {
			res = (res << 1) | chooseBit(base, bit)
		}
	}
	return res
}

// customBase64Encode gt 专用 Base64 编码。
func customBase64Encode(data []byte) string {
	length := len(data)
	result := make([]byte, 0, length*4/3+4)
	ptr := 0

	for ptr < length {
		if ptr+2 < length {
			c := (int(data[ptr]) << 16) + (int(data[ptr+1]) << 8) + int(data[ptr+2])
			result = append(result, base64Table[getIntByMask(c, mask1)])
			result = append(result, base64Table[getIntByMask(c, mask2)])
			result = append(result, base64Table[getIntByMask(c, mask3)])
			result = append(result, base64Table[getIntByMask(c, mask4)])
		} else {
			u := length % 3
			switch u {
			case 2:
				c := (int(data[ptr]) << 16) + (int(data[ptr+1]) << 8)
				result = append(result, base64Table[getIntByMask(c, mask1)])
				result = append(result, base64Table[getIntByMask(c, mask2)])
				result = append(result, base64Table[getIntByMask(c, mask3)])
				result = append(result, '.')
			case 1:
				c := int(data[ptr]) << 16
				result = append(result, base64Table[getIntByMask(c, mask1)])
				result = append(result, base64Table[getIntByMask(c, mask2)])
				result = append(result, '.', '.')
			}
		}
		ptr += 3
	}

	return string(result)
}
