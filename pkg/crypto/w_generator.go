// Package crypto 实现 gt 验证码 w 参数加密模块（RSA+AES+自定义 Base64，点选/滑动）。
// 移植自 biliTicker_gt（AGPL-3.0，https://github.com/Amorter/biliTicker_gt 的 src/w.rs）。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// ── 坐标转换 (从 pkg/image 迁移并增强) ───────────────────────────

// scaleCoordinate 将模型输出的像素坐标缩放为 gt 格式。
// 公式: round(coord / 333.375 * 10000)
func scaleCoordinate(x, y float64) (int, int) {
	scaledX := int(math.Round(x / 333.375 * 10000))
	scaledY := int(math.Round(y / 333.375 * 10000))
	return scaledX, scaledY
}

// generateClickKey 根据匹配结果生成点击坐标 key 字符串。
func generateClickKey(points [][2]float64) string {
	if len(points) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, p := range points {
		if i > 0 {
			sb.WriteByte(',')
		}
		sx, sy := scaleCoordinate(p[0], p[1])
		sb.WriteString(strconv.Itoa(sx))
		sb.WriteByte('_')
		sb.WriteString(strconv.Itoa(sy))
	}
	return sb.String()
}

// ── RSA 加密 ──────────────────────────────────────────────────────

var (
	rsaNHex = strings.Join([]string{
		"00C1E3934D1614465B33053E7F48EE4EC87B14B95EF88947713D25EECBFF7E74",
		"C7977D02DC1D9451F79DD5D1C10C29ACB6A9B4D6FB7D0A0279B6719E1772565F",
		"09AF627715919221AEF91899CAE08C0D686D748B20A3603BE2318CA6BC2B59706",
		"592A9219D0BF05C9F65023A21D2330807252AE0066D59CEEFA5F2748EA80BAB81",
	}, "")
	rsaEHex = "010001"
	aesKey  = []byte("1234567890123456")
	aesIV   = bytesRepeat(0x30, 16) // 16 字节 0x30
)

func bytesRepeat(b byte, n int) []byte {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = b
	}
	return buf
}

// ── RSA 加密 ──────────────────────────────────────────────────────

// rsaEncrypt 使用 RSA PKCS#1 v1.5 加密，返回十六进制字符串。
func rsaEncrypt(data []byte) (string, error) {
	nBytes, err := hex.DecodeString(rsaNHex)
	if err != nil {
		return "", fmt.Errorf("invalid RSA N hex: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)

	eBytes, err := hex.DecodeString(rsaEHex)
	if err != nil {
		return "", fmt.Errorf("invalid RSA E hex: %w", err)
	}
	e := new(big.Int).SetBytes(eBytes)

	pubKey := &rsa.PublicKey{N: n, E: int(e.Int64())}

	// 使用 PKCS1v15 加密
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, pubKey, data)
	if err != nil {
		return "", fmt.Errorf("RSA encrypt failed: %w", err)
	}
	return hex.EncodeToString(encrypted), nil
}

// ── AES 加密 ──────────────────────────────────────────────────────

// aesEncryptCBC 使用 AES-128-CBC 加密，PKCS7 填充。
func aesEncryptCBC(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("AES cipher creation failed: %w", err)
	}

	// PKCS7 padding
	padLen := aes.BlockSize - (len(data) % aes.BlockSize)
	padded := make([]byte, len(data)+padLen)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}

	// CBC encrypt
	result := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, aesIV)
	mode.CryptBlocks(result, padded)

	return result, nil
}

// ── 组装 w 参数 ───────────────────────────────────────────────────

// encryptPayload 加密 JSON payload 并组装 w 参数。
// w = custom_base64(AES_ciphertext) + hex(RSA_encrypted_key)
func encryptPayload(jsonStr string) (string, error) {
	// RSA 加密 AES 密钥
	u, err := rsaEncrypt(aesKey)
	if err != nil {
		return "", fmt.Errorf("RSA encrypt failed: %w", err)
	}

	// AES 加密 JSON
	h, err := aesEncryptCBC([]byte(jsonStr))
	if err != nil {
		return "", fmt.Errorf("AES encrypt failed: %w", err)
	}

	// 自定义 Base64 编码
	p := customBase64Encode(h)

	return p + u, nil
}

// clickPayload 是点选验证码的 payload 结构。
type clickPayload struct {
	Lang     string   `json:"lang"`
	PassTime int      `json:"passtime"`
	A        string   `json:"a"`
	TT       string   `json:"tt"`
	EP       *clickEP `json:"ep"`
	H9s9     string   `json:"h9s9"`
	RP       string   `json:"rp"`
}

type clickEP struct {
	V   string   `json:"v"`
	E_  bool     `json:"$_E_"`
	Me  bool     `json:"me"`
	Ven string   `json:"ven"`
	Ren string   `json:"ren"`
	Fp  []any    `json:"fp"`
	Lp  []any    `json:"lp"`
	Em  *clickEM `json:"em"`
	Tm  *clickTM `json:"tm"`
	Dnf string   `json:"dnf"`
	By  int      `json:"by"`
}

type clickEM struct {
	Ph int    `json:"ph"`
	Cp int    `json:"cp"`
	Ek string `json:"ek"`
	Wd int    `json:"wd"`
	Nt int    `json:"nt"`
	Si int    `json:"si"`
	Sc int    `json:"sc"`
}

type clickTM struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
	C int64 `json:"c"`
	D int   `json:"d"`
	E int   `json:"e"`
	F int64 `json:"f"`
	G int64 `json:"g"`
	H int64 `json:"h"`
	I int64 `json:"i"`
	J int64 `json:"j"`
	K int64 `json:"k"`
	L int64 `json:"l"`
	M int64 `json:"m"`
	N int64 `json:"n"`
	O int64 `json:"o"`
	P int64 `json:"p"`
	Q int64 `json:"q"`
	R int64 `json:"r"`
	S int64 `json:"s"`
	T int64 `json:"t"`
	U int64 `json:"u"`
}

// ClickCalculate 计算点选验证码的 w 参数。
//
//	points: 点击坐标数组，每个点为 [x, y] 像素坐标。
//	gt: 验证码 gt
//	challenge: 验证码 challenge
func ClickCalculate(points [][2]float64, gt, challenge string) (string, error) {
	key := generateClickKey(points)
	passTime := 1300 + int(time.Now().UnixNano()%700)

	challengePrefix := challenge
	if len(challenge) > 2 {
		challengePrefix = challenge[:len(challenge)-2]
	}

	// MD5(gt + challenge_prefix + pass_time)
	rpData := fmt.Sprintf("%s%s%d", gt, challengePrefix, passTime)
	rpHash := md5Hex([]byte(rpData))

	nowTs := time.Now().UnixMilli()

	payload := &clickPayload{
		Lang:     "zh-cn",
		PassTime: passTime,
		A:        key,
		TT:       "",
		EP: &clickEP{
			V:   "9.1.8-bfget5",
			E_:  false,
			Me:  true,
			Ven: "Google Inc. (Intel)",
			Ren: "ANGLE (Intel, Intel(R) HD Graphics 520 Direct3D11 vs_5_0 ps_5_0, D3D11)",
			Fp:  []any{"move", 483, 149, nowTs - 300, "pointermove"},
			Lp:  []any{"up", 657, 100, nowTs, "pointerup"},
			Em: &clickEM{
				Ph: 0, Cp: 0, Ek: "11", Wd: 1, Nt: 0, Si: 0, Sc: 0,
			},
			Tm: &clickTM{
				A: nowTs - 500, B: nowTs - 308, C: nowTs - 308,
				D: 0, E: 0, F: nowTs - 496, G: nowTs - 474,
				H: nowTs - 474, I: nowTs - 474, J: nowTs - 414,
				K: nowTs - 447, L: nowTs - 414, M: nowTs - 317,
				N: nowTs - 313, O: nowTs - 305, P: nowTs - 27,
				Q: nowTs - 27, R: nowTs - 22, S: nowTs - 21,
				T: nowTs - 21, U: nowTs - 21,
			},
			Dnf: "dnf",
			By:  0,
		},
		H9s9: "1816378497",
		RP:   rpHash,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("JSON marshal failed: %w", err)
	}

	return encryptPayload(string(jsonData))
}

// md5Hex 计算 MD5 哈希并返回十六进制字符串。
func md5Hex(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

// ── 滑动验证码特定加密 ──────────────────────────────────────────────

func slideFinalEncrypt(t string, c []byte, s string) string {
	if len(c) < 5 || s == "" {
		return t
	}

	sc := uint64(c[0])
	ac := uint64(c[2])
	mc := uint64(c[4])

	originalLen := uint64(len(t))
	result := t

	for i := 0; i <= len(s)-2; i += 2 {
		r := s[i : i+2]
		val, _ := strconv.ParseUint(r, 16, 8)
		char := byte(val)

		// 基于原始长度计算插入位置
		pos := (sc*uint64(char)*uint64(char) + ac*uint64(char) + mc) % originalLen

		// 插入字符
		result = result[:pos] + string(char) + result[pos:]
	}

	return result
}

func userResponse(distance int, challenge string) string {
	if len(challenge) < 2 {
		return ""
	}

	// 1. 处理 challenge 最后两个字符
	nStr := challenge[len(challenge)-2:]
	r := make([]int, 2)
	for i, c := range nStr {
		if c > '9' {
			r[i] = int(c - 'a' + 10)
		} else {
			r[i] = int(c - '0')
		}
	}
	n := 36*r[0] + r[1]
	a := distance + n

	// 2. 填充五元组
	underscores := make([][]rune, 5)
	charSet := make(map[rune]bool)
	idx := 0

	processedE := challenge[:len(challenge)-2]
	for _, c := range processedE {
		if !charSet[c] {
			charSet[c] = true
			underscores[idx] = append(underscores[idx], c)
			idx = (idx + 1) % 5
		}
	}

	// 3. 生成最终字符串
	f := a
	d := 4
	var res strings.Builder
	weights := []int{1, 2, 5, 10, 50}

	for f > 0 {
		if d < 0 {
			break
		}
		if f >= weights[d] && len(underscores[d]) > 0 {
			res.WriteRune(underscores[d][0])
			f -= weights[d]
		} else {
			d--
		}
	}

	return res.String()
}

// slidePayload 是滑动验证码的 payload 结构。
type slidePayload struct {
	Lang         string   `json:"lang"`
	UserResponse string   `json:"userresponse"`
	PassTime     int      `json:"passtime"`
	ImgLoad      int      `json:"imgload"`
	AA           string   `json:"aa"`
	EP           *clickEP `json:"ep"` // EP 结构通用
	RP           string   `json:"rp"`
}

// SlideCalculate 计算滑动验证码的 w 参数。
func SlideCalculate(distance int, encryptedTrack string, gt, challenge string, c []byte, s string) (string, error) {
	// 获取轨迹最后一点的时间作为 passtime
	// 这里假设加密轨迹中已经包含了时间信息，但为了方便，我们直接生成一个
	passTime := 1500 + int(time.Now().UnixNano()%500)

	aa := slideFinalEncrypt(encryptedTrack, c, s)
	uResponse := userResponse(distance, challenge)

	challengePrefix := challenge
	if len(challenge) > 2 {
		challengePrefix = challenge[:len(challenge)-2]
	}

	rpData := fmt.Sprintf("%s%s%d", gt, challengePrefix, passTime)
	rpHash := md5Hex([]byte(rpData))

	nowTs := time.Now().UnixMilli()

	payload := &slidePayload{
		Lang:         "zh-cn",
		UserResponse: uResponse,
		PassTime:     passTime,
		ImgLoad:      100 + int(time.Now().UnixNano()%100),
		AA:           aa,
		EP: &clickEP{
			V:   "9.1.8-bfget5",
			E_:  false,
			Me:  true,
			Ven: "Google Inc. (Intel)",
			Ren: "ANGLE (Intel, Intel(R) HD Graphics 520 Direct3D11 vs_5_0 ps_5_0, D3D11)",
			Fp:  []any{"move", 483, 149, nowTs - 300, "pointermove"},
			Lp:  []any{"up", 657, 100, nowTs, "pointerup"},
			Em: &clickEM{
				Ph: 0, Cp: 0, Ek: "11", Wd: 1, Nt: 0, Si: 0, Sc: 0,
			},
			Tm: &clickTM{
				A: nowTs - 500, B: nowTs - 308, C: nowTs - 308,
				D: 0, E: 0, F: nowTs - 496, G: nowTs - 474,
				H: nowTs - 474, I: nowTs - 474, J: nowTs - 414,
				K: nowTs - 447, L: nowTs - 414, M: nowTs - 317,
				N: nowTs - 313, O: nowTs - 305, P: nowTs - 27,
				Q: nowTs - 27, R: nowTs - 22, S: nowTs - 21,
				T: nowTs - 21, U: nowTs - 21,
			},
			Dnf: "dnf",
			By:  0,
		},
		RP: rpHash,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("JSON marshal failed: %w", err)
	}

	return encryptPayload(string(jsonData))
}
