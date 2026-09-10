package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
	return cfg
}

func newTestProxy(t *testing.T, stub http.HandlerFunc) (http.Handler, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", srv.URL, err)
	}
	return New(target, NewTransport(testConfig(t)), testLogger()), srv
}

func TestBackend_PreservesPathAndQuery(t *testing.T) {
	var gotURI string
	h, _ := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
	})

	req := httptest.NewRequest("GET", "/api/v1/stocks/005930/candles?period=D&limit=65", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	want := "/api/v1/stocks/005930/candles?period=D&limit=65"
	if gotURI != want {
		t.Errorf("업스트림 수신 URI = %q, want %q", gotURI, want)
	}
}

func TestBackend_PreservesKoreanEncodedPath(t *testing.T) {
	var gotURI string
	h, _ := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
	})

	const enc = "/api/v1/themes/%EC%9D%B4%EC%B0%A8%EC%A0%84%EC%A7%80"
	req := httptest.NewRequest("GET", enc, nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotURI != enc {
		t.Errorf("업스트림 수신 URI = %q, want %q (인코딩 변형 발생)", gotURI, enc)
	}
}

func TestBackend_PassesSetCookieVerbatim(t *testing.T) {
	const cookie = "refresh_token=abc123; Path=/api/v1/auth; Max-Age=1209600; HttpOnly; SameSite=Strict"
	h, _ := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", cookie)
		w.WriteHeader(200)
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/auth/login", nil))

	if got := rec.Header().Get("Set-Cookie"); got != cookie {
		t.Errorf("Set-Cookie = %q,\nwant       %q", got, cookie)
	}
}

func TestBackend_PassesGzipUntouched(t *testing.T) {
	var gotAcceptEncoding string
	var gzBody bytes.Buffer
	zw := gzip.NewWriter(&gzBody)
	if _, err := zw.Write([]byte(`{"data":[1,2,3]}`)); err != nil {
		t.Fatal(err)
	}
	zw.Close()

	h, _ := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		w.Write(gzBody.Bytes())
	})

	req := httptest.NewRequest("GET", "/api/v1/themes", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotAcceptEncoding != "gzip, deflate, br" {
		t.Errorf("업스트림 수신 Accept-Encoding = %q, want 원문 (Transport 개입 발생)", gotAcceptEncoding)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip (투명 통과 실패)", ce)
	}
	if !bytes.Equal(rec.Body.Bytes(), gzBody.Bytes()) {
		t.Error("gzip 바디가 변형됨 — 바이트 동일해야 함")
	}
}

func TestBackend_PassesUpstreamErrorBodyVerbatim(t *testing.T) {
	const errBody = `{"error":{"code":"STOCK_NOT_FOUND","message":"종목을 찾을 수 없습니다.","details":null}}`
	h, _ := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		io.WriteString(w, errBody)
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/stocks/999999", nil))

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if rec.Body.String() != errBody {
		t.Errorf("에러 바디 변형됨:\ngot  %q\nwant %q", rec.Body.String(), errBody)
	}
}

func TestBackend_TimeoutReturns503UpstreamTimeout(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	t.Cleanup(stub.Close)
	target, _ := url.Parse(stub.URL)

	cfg := testConfig(t)
	cfg.UpstreamTimeout = 50 * time.Millisecond
	h := New(target, NewTransport(cfg), testLogger())

	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/themes", nil))

	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Errorf("타임아웃 발화 지연: %v (50ms 설정)", elapsed)
	}
	if rec.Code != 503 {
		t.Errorf("status = %d, want 503 (C8: 504 금지)", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"UPSTREAM_TIMEOUT"`)) {
		t.Errorf("body = %s, want UPSTREAM_TIMEOUT code", rec.Body.String())
	}
}

func TestBackend_UnreachableReturns503UpstreamUnavailable(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	target, _ := url.Parse(stub.URL)
	stub.Close()

	h := New(target, NewTransport(testConfig(t)), testLogger())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/themes", nil))

	if rec.Code != 503 {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"UPSTREAM_UNAVAILABLE"`)) {
		t.Errorf("body = %s, want UPSTREAM_UNAVAILABLE code", rec.Body.String())
	}
}

func TestBackend_PreservesInboundXFFAndHost(t *testing.T) {
	var gotXFF, gotRealIP, gotProto, gotHost string
	h, _ := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		gotXFF = r.Header.Get("X-Forwarded-For")
		gotRealIP = r.Header.Get("X-Real-IP")
		gotProto = r.Header.Get("X-Forwarded-Proto")
		gotHost = r.Host
	})

	req := httptest.NewRequest("GET", "/api/v1/themes", nil)
	req.Host = "finngraph.example.com"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotXFF != "203.0.113.9" {
		t.Errorf("X-Forwarded-For = %q, want 무변경 통과 (Rewrite 모드 기본 제거 미대응?)", gotXFF)
	}
	if gotRealIP != "203.0.113.9" {
		t.Errorf("X-Real-IP = %q, want 통과", gotRealIP)
	}
	if gotProto != "https" {
		t.Errorf("X-Forwarded-Proto = %q, want 통과", gotProto)
	}
	if gotHost != "finngraph.example.com" {
		t.Errorf("Host = %q, want 인바운드 보존", gotHost)
	}
}

func TestKG_StripsPrefixPreservingEncoding(t *testing.T) {
	var gotURI string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
	}))
	t.Cleanup(stub.Close)
	target, _ := url.Parse(stub.URL)

	h := NewKG(target, NewTransport(testConfig(t)), testLogger())
	req := httptest.NewRequest("GET", "/kg/api/v1/themes/%EC%9D%B4%EC%B0%A8%EC%A0%84%EC%A7%80?hop=2", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	want := "/api/v1/themes/%EC%9D%B4%EC%B0%A8%EC%A0%84%EC%A7%80?hop=2"
	if gotURI != want {
		t.Errorf("kg 업스트림 수신 URI = %q, want %q", gotURI, want)
	}
}
