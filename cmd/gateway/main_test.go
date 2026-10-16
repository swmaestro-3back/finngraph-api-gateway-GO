package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/config"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/health"
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
	h := buildHandler(testConfig(t, backendStub.URL, kgStub.URL), logger, health.New())
	return h, &backendURI, &kgURI
}

func TestStack_HealthzDrainingReturns503(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hc := health.New()
	h := buildHandler(testConfig(t, "http://127.0.0.1:1", "http://127.0.0.1:1"), logger, hc)
	hc.Drain()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 503 {
		t.Errorf("/healthz = %d, want 503 (종료 대기 중)", rec.Code)
	}
}

func TestStack_HealthzAliveWithoutUpstreams(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := buildHandler(testConfig(t, "http://127.0.0.1:1", "http://127.0.0.1:1"), logger, health.New())

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
	h := buildHandler(cfg, logger, health.New())

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

func mintStackToken(t *testing.T, key *ecdsa.PrivateKey, exp time.Time) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
		Subject:   "42",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(exp),
	}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStack_JWTDisabledPassesProtectedPath(t *testing.T) {
	h, backendURI, _ := newStack(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/me", nil))

	if rec.Code != 200 || *backendURI != "/api/v1/me" {
		t.Errorf("키 미설정: status=%d backendURI=%q, want 200 통과", rec.Code, *backendURI)
	}
}

func TestStack_JWTEnforcedAtEdge(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var backendURI, backendAuth string
	backendStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendURI = r.RequestURI
		backendAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(backendStub.Close)

	cfg := testConfig(t, backendStub.URL, backendStub.URL)
	cfg.JWTPublicKey = &key.PublicKey
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := buildHandler(cfg, logger, health.New())

	t.Run("무토큰 → 401, backend 미도달", func(t *testing.T) {
		backendURI = ""
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/me", nil))
		if rec.Code != 401 || backendURI != "" {
			t.Errorf("status=%d backendURI=%q, want 401 미도달", rec.Code, backendURI)
		}
	})

	t.Run("만료 토큰 → 401 TOKEN_EXPIRED, 미도달", func(t *testing.T) {
		backendURI = ""
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+mintStackToken(t, key, time.Now().Add(-time.Minute)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 401 || !strings.Contains(rec.Body.String(), "TOKEN_EXPIRED") || backendURI != "" {
			t.Errorf("status=%d body=%s backendURI=%q", rec.Code, rec.Body.String(), backendURI)
		}
	})

	t.Run("유효 토큰 → 200, Authorization 원본 도달", func(t *testing.T) {
		token := mintStackToken(t, key, time.Now().Add(30*time.Minute))
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 || backendAuth != "Bearer "+token {
			t.Errorf("status=%d backendAuth 원본 불일치", rec.Code)
		}
	})

	t.Run("비보호 경로 무토큰 → 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/themes", nil))
		if rec.Code != 200 {
			t.Errorf("status=%d, want 200", rec.Code)
		}
	})
}

func TestStack_CORS(t *testing.T) {
	const origin = "https://app.example.com"
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var backendURI string
	backendStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendURI = r.RequestURI
		io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(backendStub.Close)

	cfg := testConfig(t, backendStub.URL, backendStub.URL)
	cfg.CORSOrigins = []string{origin}
	cfg.JWTPublicKey = &key.PublicKey
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	h := buildHandler(cfg, logger, health.New())

	corsHeadersPresent := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("Allow-Origin = %q, want %q", got, origin)
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Error("Allow-Credentials 부재")
		}
	}

	t.Run("무토큰 401에도 CORS 헤더", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		corsHeadersPresent(t, rec)
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"UNAUTHORIZED"`)) {
			t.Errorf("body = %s", rec.Body.String())
		}
	})

	t.Run("rate limit 429에도 CORS 헤더", func(t *testing.T) {
		mint := func() *http.Request {
			req := authReq()
			req.Header.Set("Origin", origin)
			return req
		}
		first := httptest.NewRecorder()
		h.ServeHTTP(first, mint())
		second := httptest.NewRecorder()
		h.ServeHTTP(second, mint())
		if second.Code != 429 {
			t.Fatalf("2회차 = %d, want 429", second.Code)
		}
		corsHeadersPresent(t, second)
	})

	t.Run("비허용 origin 401은 cors_denied와 jwt 태그 모두 기록", func(t *testing.T) {
		logBuf.Reset()
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("비허용인데 Allow-Origin 부착: %v", rec.Header())
		}
		if !bytes.Contains(logBuf.Bytes(), []byte(`"err":"cors_denied,jwt_missing"`)) {
			t.Errorf("access log = %s", logBuf.String())
		}
	})

	t.Run("preflight 204 backend 미도달", func(t *testing.T) {
		backendURI = ""
		req := httptest.NewRequest("OPTIONS", "/api/v1/auth/login", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 204 {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
		corsHeadersPresent(t, rec)
		if rec.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("Allow-Methods 부재")
		}
		if backendURI != "" {
			t.Errorf("backend에 %q 도달 — preflight 종결 실패", backendURI)
		}
	})
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
