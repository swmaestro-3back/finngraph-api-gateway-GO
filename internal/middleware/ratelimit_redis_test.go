package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/config"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

func rlRedisConfig(t *testing.T, addr string) *config.Config {
	t.Helper()
	u, _ := url.Parse("http://upstream:8080")
	return &config.Config{
		BackendURL:       u,
		KGURL:            u,
		RedisAddr:        addr,
		RateGeneralRPS:   1,
		RateGeneralBurst: 2,
		RateAuthRPM:      60,
		RateAuthBurst:    1,
		TrustProxy:       true,
	}
}

func redisChain(t *testing.T, addr string) http.Handler {
	t.Helper()
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	return RealIP(true, "auto")(RateLimit(rlRedisConfig(t, addr), testLogger(), pathClassifier)(ok))
}

func TestRateLimitRedis_BurstExhaustionReturns429(t *testing.T) {
	mr := miniredis.RunT(t)
	h := redisChain(t, mr.Addr())

	for i := range 2 {
		if rec := fire(h, "GET", "/api/v1/themes", "10.0.0.51"); rec.Code != 200 {
			t.Fatalf("요청 %d: status = %d, want 200 (burst 내)", i+1, rec.Code)
		}
	}
	rec := fire(h, "GET", "/api/v1/themes", "10.0.0.51")
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

func TestRateLimitRedis_RefillAllowsAgain(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := rlRedisConfig(t, mr.Addr())
	cfg.RateGeneralRPS = 20
	cfg.RateGeneralBurst = 1
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := RealIP(true, "auto")(RateLimit(cfg, testLogger(), pathClassifier)(ok))

	if rec := fire(h, "GET", "/api/v1/themes", "10.0.0.52"); rec.Code != 200 {
		t.Fatalf("1회차 = %d, want 200", rec.Code)
	}
	if rec := fire(h, "GET", "/api/v1/themes", "10.0.0.52"); rec.Code != 429 {
		t.Fatalf("즉시 2회차 = %d, want 429", rec.Code)
	}
	time.Sleep(120 * time.Millisecond)
	if rec := fire(h, "GET", "/api/v1/themes", "10.0.0.52"); rec.Code != 200 {
		t.Errorf("리필 후 = %d, want 200 (20rps면 120ms에 토큰 2개 리필)", rec.Code)
	}
}

func TestRateLimitRedis_PerIPIsolation(t *testing.T) {
	mr := miniredis.RunT(t)
	h := redisChain(t, mr.Addr())

	for range 3 {
		fire(h, "GET", "/api/v1/themes", "10.0.0.53")
	}
	if rec := fire(h, "GET", "/api/v1/themes", "10.0.0.99"); rec.Code != 200 {
		t.Errorf("다른 IP: status = %d, want 200 (버킷 격리 실패)", rec.Code)
	}
}

func TestRateLimitRedis_AuthBucketIndependent(t *testing.T) {
	mr := miniredis.RunT(t)
	h := redisChain(t, mr.Addr())

	if rec := fire(h, "POST", "/api/v1/auth/login", "10.0.0.54"); rec.Code != 200 {
		t.Fatalf("auth 1회차 = %d, want 200", rec.Code)
	}
	if rec := fire(h, "POST", "/api/v1/auth/login", "10.0.0.54"); rec.Code != 429 {
		t.Errorf("auth burst(1) 초과 = %d, want 429", rec.Code)
	}
	if rec := fire(h, "GET", "/api/v1/themes", "10.0.0.54"); rec.Code != 200 {
		t.Errorf("auth 소진이 general에 전염: %d, want 200", rec.Code)
	}
}

func TestRateLimitRedis_FailOpen(t *testing.T) {
	mr := miniredis.RunT(t)
	h := redisChain(t, mr.Addr())
	mr.Close()

	req := httptest.NewRequest("GET", "/api/v1/themes", nil)
	req.Header.Set("X-Real-IP", "10.0.0.55")
	req = gwerr.WithTag(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("Redis 다운 시 status = %d, want 200 (fail-open)", rec.Code)
	}
}

func TestRateLimitRedis_KeyHasTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	h := redisChain(t, mr.Addr())

	fire(h, "GET", "/api/v1/themes", "10.0.0.56")

	key := "gw:rl:general:10.0.0.56"
	if !mr.Exists(key) {
		t.Fatalf("키 %q 부재 — 키 형식 변경?", key)
	}
	if ttl := mr.TTL(key); ttl <= 0 {
		t.Errorf("TTL = %v, want 양수 (idle 자동 소멸)", ttl)
	}
}
