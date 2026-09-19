package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/config"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

func rlConfig() *config.Config {
	u, _ := url.Parse("http://upstream:8080")
	return &config.Config{
		BackendURL:       u,
		KGURL:            u,
		RateGeneralRPS:   1,
		RateGeneralBurst: 2,
		RateAuthRPM:      60,
		RateAuthBurst:    1,
		TrustProxy:       true,
	}
}

func pathClassifier(r *http.Request) Class {
	p := r.URL.EscapedPath()
	switch {
	case p == "/healthz":
		return ClassNone
	case r.Method == "POST" && len(p) >= 13 && p[:13] == "/api/v1/auth/":
		return ClassAuth
	default:
		return ClassGeneral
	}
}

func rlChain(cfg *config.Config) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	return RealIP(true, "auto")(RateLimit(cfg, testLogger(), pathClassifier)(ok))
}

func fire(h http.Handler, method, path, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Real-IP", ip)
	req = gwerr.WithTag(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRateLimit_BurstExhaustionReturns429(t *testing.T) {
	h := rlChain(rlConfig())

	for i := range 2 {
		if rec := fire(h, "GET", "/api/v1/themes", "10.0.0.1"); rec.Code != 200 {
			t.Fatalf("요청 %d: status = %d, want 200 (burst 내)", i+1, rec.Code)
		}
	}
	rec := fire(h, "GET", "/api/v1/themes", "10.0.0.1")
	if rec.Code != 429 {
		t.Fatalf("burst 초과: status = %d, want 429", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"RATE_LIMITED"`)) {
		t.Errorf("body = %s, want RATE_LIMITED envelope", rec.Body.String())
	}
	ra, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || ra < 1 {
		t.Errorf("Retry-After = %q, want 1 이상의 정수", rec.Header().Get("Retry-After"))
	}
}

func TestRateLimit_SetsErrTag(t *testing.T) {
	h := rlChain(rlConfig())
	fire(h, "GET", "/api/v1/themes", "10.0.0.2")
	fire(h, "GET", "/api/v1/themes", "10.0.0.2")

	req := httptest.NewRequest("GET", "/api/v1/themes", nil)
	req.Header.Set("X-Real-IP", "10.0.0.2")
	req = gwerr.WithTag(req)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got := gwerr.Tag(req); got != "rate_limited" {
		t.Errorf("err tag = %q, want rate_limited", got)
	}
}

func TestRateLimit_PerIPIsolation(t *testing.T) {
	h := rlChain(rlConfig())
	fire(h, "GET", "/api/v1/themes", "10.0.0.3")
	fire(h, "GET", "/api/v1/themes", "10.0.0.3")
	fire(h, "GET", "/api/v1/themes", "10.0.0.3")

	if rec := fire(h, "GET", "/api/v1/themes", "10.0.0.99"); rec.Code != 200 {
		t.Errorf("다른 IP: status = %d, want 200 (버킷 격리 실패)", rec.Code)
	}
}

func TestRateLimit_AuthBucketIndependent(t *testing.T) {
	h := rlChain(rlConfig())

	if rec := fire(h, "POST", "/api/v1/auth/login", "10.0.0.4"); rec.Code != 200 {
		t.Fatalf("auth 1회차: status = %d, want 200", rec.Code)
	}
	if rec := fire(h, "POST", "/api/v1/auth/login", "10.0.0.4"); rec.Code != 429 {
		t.Errorf("auth burst(1) 초과: status = %d, want 429", rec.Code)
	}
	if rec := fire(h, "GET", "/api/v1/themes", "10.0.0.4"); rec.Code != 200 {
		t.Errorf("auth 소진이 general에 전염: status = %d, want 200", rec.Code)
	}
}

func TestRateLimit_ClassNoneBypassesLimit(t *testing.T) {
	h := rlChain(rlConfig())
	for i := range 10 {
		if rec := fire(h, "GET", "/healthz", "10.0.0.5"); rec.Code != 200 {
			t.Fatalf("healthz %d회차: status = %d, want 200 (리밋 비적용)", i+1, rec.Code)
		}
	}
}

func TestPool_SweepEvictsIdleEntries(t *testing.T) {
	p := newPool(1, 1)
	p.get("10.0.0.6")
	p.get("10.0.0.7")

	p.mu.Lock()
	p.entries["10.0.0.6"].lastSeen = time.Now().Add(-11 * time.Minute)
	p.mu.Unlock()

	p.sweep(10 * time.Minute)

	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.entries["10.0.0.6"]; ok {
		t.Error("idle 10분 초과 엔트리가 evict되지 않음")
	}
	if _, ok := p.entries["10.0.0.7"]; !ok {
		t.Error("활성 엔트리가 잘못 evict됨")
	}
}
