// Package client 是本库的【网络外层】——唯一触网（net/http）的包。
//
// 它编排 gt V3 协议（点选 + 滑动）：申请 c/s → 判定类型 → 取本轮参数与图像 → 调用纯本地
// 的求解层（点选走 pkg/solver 的 wasm 推理，滑动走 pkg/solver/classic 的图像处理）→ 用
// pkg/crypto 计算 w → 提交并取回 validate。与纯本地层界限分明：本地层完全离线可测，本包是
// 薄而可替换的编排器。
//
// V3Client 是可复用的（配置一次，多次求解不同 gt/challenge）；gt/challenge 属于单次调用而
// 非客户端状态。协议流程移植自 biliTicker_gt（AGPL-3.0，
// https://github.com/Amorter/biliTicker_gt）的 src/click.rs / src/slide.rs / src/abstraction.rs。
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cca2878/gtlv-go/pkg/solver"
)

const (
	// defaultVerifyDelay 是本轮验证码「签发 → 提交 verify」应满足的最小墙钟时长。
	// gt 按此校验 passtime，过快必判机器。这是【总时长下限】而非固定睡眠：wasm 推理与
	// 网络本身已耗时，故只补足差额（见 session.sleepUntil），不盲目再睡满。
	defaultVerifyDelay = 2 * time.Second
	// defaultTimeout 是默认 http.Client 的超时。
	defaultTimeout = 15 * time.Second

	defaultGetHost   = "api.geetest.com"
	defaultVisitHost = "api.geevisit.com"
)

// V3Client 是可复用的 gt V3 验证码客户端，支持点选与滑动。
// 它只承载配置（主机、http.Client、重试、时延），不含任何单次验证码的状态，
// 因此可安全地被多个 goroutine 共享、对不同 gt/challenge 反复调用 GetValidate。
// 零值不可用，请用 NewV3Client 构造。
type V3Client struct {
	getHost     string
	visitHost   string
	httpClient  *http.Client
	maxAttempts int
	verifyDelay time.Duration
}

// Option 配置 V3Client。
type Option func(*V3Client)

// WithHTTPClient 使用自定义 http.Client（超时/代理/Cookie）。nil 忽略。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *V3Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithHosts 覆盖 gt 主机（getHost 用于首个 get.php，visitHost 用于 get/ajax/refresh）。
// 任一参数为空则保留默认。
func WithHosts(getHost, visitHost string) Option {
	return func(c *V3Client) {
		if getHost != "" {
			c.getHost = getHost
		}
		if visitHost != "" {
			c.visitHost = visitHost
		}
	}
}

// WithMaxAttempts 设置单次 GetValidate 内的最多尝试次数（含首次，即「最多重试 n-1 次」）。
// 仅当失败可重试（识别未通过 / 图内容不佳）时才会重试，每次重试都换取新图。
// n<1 归一为 1（不重试）。
func WithMaxAttempts(n int) Option {
	return func(c *V3Client) {
		if n < 1 {
			n = 1
		}
		c.maxAttempts = n
	}
}

// WithVerifyDelay 覆盖「签发 → 提交」的最小墙钟时长（默认 2s）。<0 归一为 0。
// 调低会加大被 gt 判为机器的风险；一般无需改动，测试可置 0 提速。
func WithVerifyDelay(d time.Duration) Option {
	return func(c *V3Client) {
		if d < 0 {
			d = 0
		}
		c.verifyDelay = d
	}
}

// NewV3Client 创建一个可复用的 V3Client。
func NewV3Client(opts ...Option) *V3Client {
	c := &V3Client{
		getHost:     defaultGetHost,
		visitHost:   defaultVisitHost,
		maxAttempts: 1,
		verifyDelay: defaultVerifyDelay,
		httpClient:  &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// GetValidate 对给定 gt/challenge 执行完整验证流并返回 validate：
// 申请 c/s → 判定类型 → 分派到点选或滑动路径（失败可换图重试）。
//
// gt/challenge 由调用方从各自的业务接口获取（自测见 Register）。
// clickSolver 仅在识别为「点选」时需要（可为 nil，此时遇到点选返回 ErrSolverRequired）；
// 「滑动」为纯本地图像处理，不需要它。
func (c *V3Client) GetValidate(ctx context.Context, gt, challenge string, clickSolver *solver.CaptchaSolver) (string, error) {
	s := &session{c: c, gt: gt, challenge: challenge}

	// 1. 申请初始 c/s（仅为建立会话；点选/滑动都用各自 get.php 返回的参数，故此处丢弃）。
	if _, _, err := s.getCS(ctx); err != nil {
		return "", fmt.Errorf("get c/s failed: %w", err)
	}

	// 2. 判定验证码类型。
	typ, err := s.getType(ctx)
	if err != nil {
		return "", fmt.Errorf("get type failed: %w", err)
	}

	switch typ {
	case "click":
		if clickSolver == nil {
			return "", ErrSolverRequired
		}
		return s.clickValidate(ctx, clickSolver)
	case "slide":
		return s.slideValidate(ctx)
	default:
		return "", &UnsupportedCaptchaTypeError{Type: typ}
	}
}

// session 承载单次 GetValidate 的每次调用状态（gt、challenge）。
// challenge 在滑动路径会被服务端下发的新值覆盖。
type session struct {
	c         *V3Client
	gt        string
	challenge string
}

// getCS 申请初始 c/s。对应 abstraction.rs::get_c_s。
func (s *session) getCS(ctx context.Context) ([]byte, string, error) {
	q := url.Values{}
	q.Set("gt", s.gt)
	q.Set("challenge", s.challenge)
	data, err := s.c.getJSONP(ctx, "https://"+s.c.getHost+"/get.php", q)
	if err != nil {
		return nil, "", err
	}
	cBytes, err := jsonBytes(data, "c")
	if err != nil {
		return nil, "", err
	}
	str, err := jsonString(data, "s")
	if err != nil {
		return nil, "", err
	}
	return cBytes, str, nil
}

// getType 返回验证码类型（"click"/"slide"）。对应 abstraction.rs::get_type。
func (s *session) getType(ctx context.Context) (string, error) {
	q := url.Values{}
	q.Set("gt", s.gt)
	q.Set("challenge", s.challenge)
	data, err := s.c.getJSONP(ctx, "http://"+s.c.visitHost+"/ajax.php", q)
	if err != nil {
		return "", err
	}
	return jsonString(data, "result")
}

// verify 提交 w 并取回 validate。点选/滑动共用（同一 ajax.php 端点）。
// challenge 显式传入（滑动用服务端下发的新 challenge）。
func (s *session) verify(ctx context.Context, challenge, w string) (string, error) {
	q := url.Values{}
	q.Set("gt", s.gt)
	q.Set("challenge", challenge)
	q.Set("w", w)
	data, err := s.c.getJSONP(ctx, "http://"+s.c.visitHost+"/ajax.php", q)
	if err != nil {
		return "", err
	}
	// 失败时 gt 回 result="fail"（滑动为 message!="success"）且 validate 缺失/为空。
	result, _ := jsonString(data, "result")
	message, _ := jsonString(data, "message")
	if (result != "" && result != "success") || (message != "" && message != "success") {
		return "", &VerifyError{Result: result, Message: message}
	}
	validate, _ := jsonString(data, "validate")
	if validate == "" {
		// result/message 未明示失败却拿不到 validate，同样归为验证失败（可重试）。
		return "", &VerifyError{Result: result, Message: message}
	}
	return validate, nil
}

// sleepUntil 睡到 issued+verifyDelay 那一刻；若已过则立即返回。响应 ctx 取消。
func (s *session) sleepUntil(ctx context.Context, issued time.Time) error {
	remaining := time.Until(issued.Add(s.c.verifyDelay))
	if remaining <= 0 {
		return nil
	}
	t := time.NewTimer(remaining)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ── 与单次状态无关的网络/解析辅助（挂在 V3Client 或包级）─────────────

// download 拉取图片字节。对应 abstraction.rs::download_img。
func (c *V3Client) download(ctx context.Context, imgURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// getJSONP 发起 GET，注入唯一 callback，剥掉 JSONP 包裹，返回 data 子对象。
// gt 所有 get/ajax 端点均以 geetest_<callback>(...) 形式返回，data 内含真正字段。
func (c *V3Client) getJSONP(ctx context.Context, endpoint string, q url.Values) (map[string]any, error) {
	callback := "geetest_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	q.Set("callback", callback)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	inner, err := unwrapJSONP(string(body), callback)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal([]byte(inner), &root); err != nil {
		return nil, fmt.Errorf("parse json: %w (body=%.120q)", err, inner)
	}
	data, ok := root["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing 'data' object (body=%.120q)", inner)
	}
	return data, nil
}

// unwrapJSONP 剥掉 `<callback>( ... )` 包裹，返回内层 JSON 文本。
func unwrapJSONP(body, callback string) (string, error) {
	prefix := callback + "("
	inner, ok := strings.CutPrefix(strings.TrimSpace(body), prefix)
	if !ok {
		return "", fmt.Errorf("jsonp prefix mismatch (want %q, body=%.80q)", prefix, body)
	}
	inner, ok = strings.CutSuffix(strings.TrimSpace(inner), ")")
	if !ok {
		return "", fmt.Errorf("jsonp suffix mismatch (body=%.80q)", body)
	}
	return inner, nil
}

// jsonString 从 map 取字符串字段。
func jsonString(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("missing field %q", key)
	}
	str, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field %q is not a string", key)
	}
	return str, nil
}

// jsonBytes 从 map 取数值数组字段（如 c）并转为 []byte。
func jsonBytes(m map[string]any, key string) ([]byte, error) {
	v, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("missing field %q", key)
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("field %q is not an array", key)
	}
	out := make([]byte, len(arr))
	for i, e := range arr {
		f, ok := e.(float64)
		if !ok {
			return nil, fmt.Errorf("field %q[%d] is not a number", key, i)
		}
		out[i] = byte(int(f))
	}
	return out, nil
}

// joinImageURL 把 static_servers[0] 与某图片路径拼成 URL（去 pic 前导斜杠）。
func joinImageURL(m map[string]any, serversKey, picKey string) (string, error) {
	servers, ok := m[serversKey].([]any)
	if !ok || len(servers) == 0 {
		return "", fmt.Errorf("missing/empty %q", serversKey)
	}
	server, ok := servers[0].(string)
	if !ok || server == "" {
		return "", fmt.Errorf("%q[0] is not a non-empty string", serversKey)
	}
	pic, err := jsonString(m, picKey)
	if err != nil {
		return "", err
	}
	return "https://" + server + strings.TrimPrefix(pic, "/"), nil
}

// newImageQuery 构造 get.php 取图参数（点选/滑动仅 type 不同）。
func newImageQuery(gt, challenge, typ string) url.Values {
	q := url.Values{}
	q.Set("gt", gt)
	q.Set("challenge", challenge)
	q.Set("is_next", "true")
	q.Set("offline", "false")
	q.Set("isPC", "true")
	q.Set("type", typ)
	return q
}
