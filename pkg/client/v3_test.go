package client

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUnwrapJSONP(t *testing.T) {
	cb := "geetest_123"
	got, err := unwrapJSONP(cb+`({"status":"success"})`, cb)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != `{"status":"success"}` {
		t.Fatalf("got %q", got)
	}

	if _, err := unwrapJSONP(`wrong({})`, cb); err == nil {
		t.Fatal("want prefix-mismatch error")
	}
	if _, err := unwrapJSONP(cb+`({}`, cb); err == nil {
		t.Fatal("want suffix-mismatch error")
	}
}

func TestJSONBytes(t *testing.T) {
	// JSON 反序列化后数值均为 float64，jsonBytes 需把它们收成 []byte。
	m := map[string]any{"c": []any{float64(12), float64(34), float64(255)}}
	got, err := jsonBytes(m, "c")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 12 || got[2] != 255 {
		t.Fatalf("got %v", got)
	}
	if _, err := jsonBytes(map[string]any{}, "c"); err == nil {
		t.Fatal("want missing-field error")
	}
}

func TestJoinImageURL(t *testing.T) {
	m := map[string]any{
		"static_servers": []any{"static.geetest.com/", "backup/"},
		"pic":            "/pictures/gt/abc/abc.jpg",
	}
	got, err := joinImageURL(m, "static_servers", "pic")
	if err != nil {
		t.Fatal(err)
	}
	// server 保留其尾斜杠，pic 去掉前导斜杠，恰好拼成合法 URL。
	if got != "https://static.geetest.com/pictures/gt/abc/abc.jpg" {
		t.Fatalf("got %q", got)
	}

	if _, err := joinImageURL(map[string]any{"static_servers": []any{}}, "static_servers", "pic"); err == nil {
		t.Fatal("want empty-servers error")
	}
}

// TestGetJSONP 用 httptest 模拟极验 JSONP 端点，验证网络管道：callback 注入、
// 包裹剥离、data 提取，均不触真实网络。
func TestGetJSONP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cb := r.URL.Query().Get("callback")
		if cb == "" || !strings.HasPrefix(cb, "geetest_") {
			t.Errorf("missing/invalid callback: %q", cb)
		}
		_, _ = w.Write([]byte(cb + `({"status":"success","data":{"result":"click"}})`))
	}))
	defer srv.Close()

	c := NewV3Client()
	data, err := c.getJSONP(context.Background(), srv.URL+"/ajax.php", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if data["result"] != "click" {
		t.Fatalf("got %v", data["result"])
	}
}

// TestClickImageAndVerify 走 getClickImage / verify 两条真实路径（除网络外全链路）。
func TestClickImageAndVerify(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/get.php", func(w http.ResponseWriter, r *http.Request) {
		cb := r.URL.Query().Get("callback")
		if r.URL.Query().Get("type") != "click" {
			t.Errorf("type param not forwarded: %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(cb + `({"data":{"static_servers":["static.geetest.com/"],` +
			`"pic":"/pics/x.jpg","s":"abc","c":[1,2,3]}})`))
	})
	mux.HandleFunc("/ajax.php", func(w http.ResponseWriter, r *http.Request) {
		cb := r.URL.Query().Get("callback")
		if r.URL.Query().Get("w") != "WPARAM" {
			t.Errorf("w not forwarded: %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(cb + `({"data":{"result":"success","validate":"THE_VALIDATE"}})`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	s := &session{c: NewV3Client(WithHosts(host, host)), gt: "GT", challenge: "CH"}

	picURL, err := s.getClickImage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if picURL != "https://static.geetest.com/pics/x.jpg" {
		t.Fatalf("picURL = %q", picURL)
	}

	validate, err := s.verify(context.Background(), "CH", "WPARAM")
	if err != nil {
		t.Fatal(err)
	}
	if validate != "THE_VALIDATE" {
		t.Fatalf("validate = %q", validate)
	}
}

// TestGetSlideParams 验证滑动取参：新 challenge、c/s、fullbg/bg 三项拼 URL。
func TestGetSlideParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cb := r.URL.Query().Get("callback")
		if r.URL.Query().Get("type") != "slide" {
			t.Errorf("type param not slide: %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(cb + `({"data":{"static_servers":["static.geetest.com/"],` +
			`"challenge":"NEWCH","c":[1,2,3],"s":"sss",` +
			`"fullbg":"/full.png","bg":"/bg.png","slice":"/slice.png"}})`))
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	s := &session{c: NewV3Client(WithHosts(host, host)), gt: "GT", challenge: "OLDCH"}

	p, err := s.getSlideParams(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.newChallenge != "NEWCH" {
		t.Errorf("newChallenge = %q", p.newChallenge)
	}
	if p.s != "sss" || len(p.c) != 3 {
		t.Errorf("c/s = %v / %q", p.c, p.s)
	}
	if p.fullbgURL != "https://static.geetest.com/full.png" {
		t.Errorf("fullbgURL = %q", p.fullbgURL)
	}
	if p.bgURL != "https://static.geetest.com/bg.png" {
		t.Errorf("bgURL = %q", p.bgURL)
	}
}

// TestVerifyFailure 确认失败响应被明确报错，且暴露为可判定的类型化错误。
func TestVerifyFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cb := r.URL.Query().Get("callback")
		_, _ = w.Write([]byte(cb + `({"data":{"result":"fail"}})`))
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	s := &session{c: NewV3Client(WithHosts(host, host)), gt: "GT", challenge: "CH"}

	_, err := s.verify(context.Background(), "CH", "W")
	if err == nil {
		t.Fatal("want failure error")
	}
	if !errors.Is(err, ErrVerificationFailed) {
		t.Errorf("errors.Is(err, ErrVerificationFailed) = false; err=%v", err)
	}
	var ve *VerifyError
	if !errors.As(err, &ve) {
		t.Fatalf("errors.As(*VerifyError) = false; err=%v", err)
	}
	if ve.Result != "fail" {
		t.Errorf("VerifyError.Result = %q, want \"fail\"", ve.Result)
	}
}

// solidPNG 生成一张纯色 320x160 PNG（滑动求解要求源图 ≥310 宽以供还原）。
func solidPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 320, 160))
	for y := range 160 {
		for x := range 320 {
			img.Set(x, y, c)
		}
	}
	var buf strings.Builder
	if err := png.Encode(stringWriter{&buf}, img); err != nil {
		t.Fatal(err)
	}
	return []byte(buf.String())
}

type stringWriter struct{ b *strings.Builder }

func (w stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// TestSlideRetry 驱动滑动重试：verify 恒失败，断言每次尝试都重取参数、总尝试数=maxAttempts、
// 且最终返回可判定的 ErrVerificationFailed。全程 httptest，不触网、不需 wasm。
func TestSlideRetry(t *testing.T) {
	// 缺口图与完整图差异明显，保证本地缺口识别得到正距离（SolveSlide 不报错）。
	full := solidPNG(t, color.RGBA{200, 200, 200, 255})
	gap := solidPNG(t, color.RGBA{10, 10, 10, 255})

	var getCalls, verifyCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/get.php", func(w http.ResponseWriter, r *http.Request) {
		getCalls.Add(1)
		cb := r.URL.Query().Get("callback")
		_, _ = w.Write([]byte(cb + `({"data":{"static_servers":["HOST/"],` +
			`"challenge":"NEWCH","c":[1,2,3,4,5],"s":"sss",` +
			`"fullbg":"/full.png","bg":"/bg.png","slice":"/slice.png"}})`))
	})
	mux.HandleFunc("/ajax.php", func(w http.ResponseWriter, r *http.Request) {
		verifyCalls.Add(1)
		cb := r.URL.Query().Get("callback")
		_, _ = w.Write([]byte(cb + `({"data":{"message":"fail"}})`))
	})
	mux.HandleFunc("/full.png", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(full) })
	mux.HandleFunc("/bg.png", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(gap) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	// 图 URL 以 https://HOST/ 拼出，改写主机使其回到测试服务器。
	hc := &http.Client{Transport: rewriteHost{host}}
	c := NewV3Client(WithHosts(host, host), WithHTTPClient(hc), WithMaxAttempts(3), WithVerifyDelay(0))
	s := &session{c: c, gt: "GT", challenge: "CH"}

	_, err := s.slideValidate(context.Background())
	if !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("want ErrVerificationFailed, got %v", err)
	}
	if got := getCalls.Load(); got != 3 {
		t.Errorf("get.php 调用 %d 次，期望 3（每次尝试重取参数）", got)
	}
	if got := verifyCalls.Load(); got != 3 {
		t.Errorf("ajax.php 调用 %d 次，期望 3", got)
	}
}

// rewriteHost 把图片 URL 里的占位主机 "HOST" 改写到测试服务器。
type rewriteHost struct{ host string }

func (rt rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "HOST" {
		req.URL.Scheme = "http"
		req.URL.Host = rt.host
	}
	return http.DefaultTransport.RoundTrip(req)
}
