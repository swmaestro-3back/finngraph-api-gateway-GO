package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/config"
)

func testConfig(t *testing.T, backend, kg string) *config.Config {
	t.Helper()
	bu, err := url.Parse(backend)
	if err != nil {
		t.Fatal(err)
	}
	ku, err := url.Parse(kg)
	if err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		ListenAddr:       ":0",
		BackendURL:       bu,
		KGURL:            ku,
		UpstreamTimeout:  2_000_000_000,
		DialTimeout:      1_000_000_000,
		MaxConnsPerHost:  16,
		RateGeneralRPS:   1000,
		RateGeneralBurst: 1000,
		RateAuthRPM:      60,
		RateAuthBurst:    1,
		TrustProxy:       true,
	}
}

func newStack(t *testing.T) (http.Handler, *string, *string) {
	t.Helper()
	var backendURI, kgURI string
	backendStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(backendStub.Close)
	kgStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kgURI = r.RequestURI
		io.WriteString(w, `{"companies":[]}`)
	}))
	t.Cleanup(kgStub.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := buildHandler(testConfig(t, backendStub.URL, kgStub.URL), logger)
	return h, &backendURI, &kgURI
}

func TestStack_HealthzAliveWithoutUpstreams(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := buildHandler(testConfig(t, "http://127.0.0.1:1", "http://127.0.0.1:1"), logger)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Errorf("/healthz = %d, want 200 (업스트림 다운과 무관)", rec.Code)
	}
}

func TestStack_APIFlowsToBackendWithRequestID(t *testing.T) {
	h, backendURI, _ := newStack(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/themes?page=0", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if *backendURI != "/api/v1/themes?page=0" {
		t.Errorf("backend 수신 URI = %q", *backendURI)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id 응답 에코 없음")
	}
}

func TestStack_KGStripsPrefix(t *testing.T) {
	h, _, kgURI := newStack(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/kg/api/v1/companies/005930", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if *kgURI != "/api/v1/companies/005930" {
		t.Errorf("kg 수신 URI = %q, want /kg 스트립", *kgURI)
	}
}

func TestStack_ActuatorBlocked(t *testing.T) {
	h, backendURI, _ := newStack(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/actuator/prometheus", nil))

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if *backendURI != "" {
		t.Errorf("backend에 %q 도달 — 차단 실패", *backendURI)
	}
}

func TestStack_AuthRateLimitStricter(t *testing.T) {
	h, _, _ := newStack(t)

	first := httptest.NewRecorder()
	h.ServeHTTP(first, authReq())
	if first.Code != 200 {
		t.Fatalf("auth 1회차 = %d, want 200", first.Code)
	}
	second := httptest.NewRecorder()
	h.ServeHTTP(second, authReq())
	if second.Code != 429 {
		t.Errorf("auth 2회차 = %d, want 429 (burst 1)", second.Code)
	}
	general := httptest.NewRecorder()
	h.ServeHTTP(general, httptest.NewRequest("GET", "/api/v1/themes", nil))
	if general.Code != 200 {
		t.Errorf("general = %d, want 200 (auth 소진 비전염)", general.Code)
	}
}

func authReq() *http.Request {
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("X-Real-IP", "10.9.9.9")
	return req
}

func TestStack_BodyLimitReturns413(t *testing.T) {
	h, _, _ := newStack(t)

	big := bytes.Repeat([]byte("a"), 2<<20)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(big))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 413 {
		t.Errorf("2MiB body = %d, want 413", rec.Code)
	}
}

func TestStack_DeadlineCapsConnSlotWait(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(slow.Close)

	cfg := testConfig(t, slow.URL, slow.URL)
	cfg.UpstreamTimeout = 200 * time.Millisecond
	cfg.MaxConnsPerHost = 1
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := buildHandler(cfg, logger)

	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/themes", nil))
	}()
	time.Sleep(30 * time.Millisecond)

	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/stocks", nil))
	elapsed := time.Since(start)

	if rec.Code != 503 {
		t.Errorf("슬롯 대기 요청 status = %d, want 503", rec.Code)
	}
	if elapsed > 350*time.Millisecond {
		t.Errorf("슬롯 대기 요청 소요 %v — 데드라인(200ms)이 대기를 못 끊음", elapsed)
	}
}

func TestProbeURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{":8080", "http://127.0.0.1:8080/healthz"},
		{"0.0.0.0:8090", "http://127.0.0.1:8090/healthz"},
		{"127.0.0.1:9000", "http://127.0.0.1:9000/healthz"},
	} {
		if got := probeURL(tc.in); got != tc.want {
			t.Errorf("probeURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRunHealthcheck(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(up.Close)

	if err := runHealthcheck(up.URL + "/healthz"); err != nil {
		t.Errorf("정상 서버: %v, want nil", err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(down.Close)
	if err := runHealthcheck(down.URL + "/healthz"); err == nil {
		t.Error("500 서버: want error")
	}
}
