package router

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/middleware"
)

type called struct{ name string }

func mark(name string, c *called) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.name = name
		w.WriteHeader(200)
	})
}

func newTestRouter() (http.Handler, *called) {
	c := &called{}
	return New(mark("backend", c), mark("kg", c), mark("healthz", c)), c
}

func send(h http.Handler, method, path string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(method, path, nil)
	req = gwerr.WithTag(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, req
}

func TestRouter_APIRoutesToBackend(t *testing.T) {
	h, c := newTestRouter()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/themes"},
		{"POST", "/api/v1/auth/login"},
		{"GET", "/api/v1/stocks/005930/candles?period=D"},
		{"GET", "/api/v1/unknown/future/endpoint"},
	} {
		c.name = ""
		if rec, _ := send(h, tc.method, tc.path); rec.Code != 200 || c.name != "backend" {
			t.Errorf("%s %s → handler=%q status=%d, want backend 200", tc.method, tc.path, c.name, rec.Code)
		}
	}
}

func TestRouter_KGRoutesOnGET(t *testing.T) {
	h, c := newTestRouter()
	if rec, _ := send(h, "GET", "/kg/api/v1/companies/005930"); rec.Code != 200 || c.name != "kg" {
		t.Errorf("kg GET → handler=%q status=%d, want kg 200", c.name, rec.Code)
	}
}

func TestRouter_KGNonGETReturns405(t *testing.T) {
	h, c := newTestRouter()
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		c.name = ""
		rec, req := send(h, method, "/kg/api/v1/companies/005930")
		if rec.Code != 405 {
			t.Errorf("kg %s → status=%d, want 405", method, rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET" {
			t.Errorf("kg %s → Allow=%q, want GET", method, allow)
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"METHOD_NOT_ALLOWED"`)) {
			t.Errorf("kg %s → body=%s, want METHOD_NOT_ALLOWED envelope", method, rec.Body.String())
		}
		if c.name != "" {
			t.Errorf("kg %s → 핸들러 %q 호출됨, want 미호출", method, c.name)
		}
		if gwerr.Tag(req) != "blocked_path" {
			t.Errorf("kg %s → tag=%q, want blocked_path", method, gwerr.Tag(req))
		}
	}
}

func TestRouter_BareKGFallsThrough(t *testing.T) {
	h, c := newTestRouter()
	if rec, _ := send(h, "GET", "/kg"); rec.Code != 404 || c.name != "" {
		t.Errorf("bare /kg → handler=%q status=%d, want 미호출 404", c.name, rec.Code)
	}
}

func TestRouter_ActuatorBlocked(t *testing.T) {
	h, c := newTestRouter()
	for _, path := range []string{
		"/actuator/health",
		"/actuator/prometheus",
		"/actuator",
		"//actuator/health",
		"/%61ctuator/health",
	} {
		c.name = ""
		rec, req := send(h, "GET", path)
		if rec.Code != 404 {
			t.Errorf("%s → status=%d, want 404", path, rec.Code)
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"NOT_FOUND"`)) {
			t.Errorf("%s → body=%s, want NOT_FOUND envelope", path, rec.Body.String())
		}
		if c.name != "" {
			t.Errorf("%s → 핸들러 %q 호출됨, want 미호출", path, c.name)
		}
		if gwerr.Tag(req) != "blocked_path" {
			t.Errorf("%s → tag=%q, want blocked_path", path, gwerr.Tag(req))
		}
	}
}

func TestRouter_DotSegmentsRejected(t *testing.T) {
	h, c := newTestRouter()
	for _, path := range []string{
		"/api/../actuator/health",
		"/api/%2e%2e/actuator/health",
		"/api/v1/./themes",
		"/kg/../actuator/health",
	} {
		c.name = ""
		rec, req := send(h, "GET", path)
		if rec.Code != 404 {
			t.Errorf("%s → status=%d, want 404 (dot-segment 거부)", path, rec.Code)
		}
		if c.name != "" {
			t.Errorf("%s → 핸들러 %q 호출됨, want 미호출", path, c.name)
		}
		if gwerr.Tag(req) != "blocked_path" {
			t.Errorf("%s → tag=%q, want blocked_path", path, gwerr.Tag(req))
		}
	}
}

func TestRouter_DotsInsideSegmentStillPass(t *testing.T) {
	h, c := newTestRouter()
	if rec, _ := send(h, "GET", "/api/v1/themes/a..b"); rec.Code != 200 || c.name != "backend" {
		t.Errorf("세그먼트 내부의 점(a..b) → handler=%q status=%d, want backend 200 (오탐 금지)", c.name, rec.Code)
	}
}

func TestRouter_HealthzExactOnly(t *testing.T) {
	h, c := newTestRouter()
	if rec, _ := send(h, "GET", "/healthz"); rec.Code != 200 || c.name != "healthz" {
		t.Errorf("/healthz → handler=%q status=%d, want healthz 200", c.name, rec.Code)
	}
	c.name = ""
	if rec, _ := send(h, "GET", "/healthz/deep"); rec.Code != 404 || c.name != "" {
		t.Errorf("/healthz/deep → handler=%q status=%d, want 미호출 404", c.name, rec.Code)
	}
}

func TestRouter_FallbackReturns404(t *testing.T) {
	h, c := newTestRouter()
	rec, req := send(h, "GET", "/favicon.ico")
	if rec.Code != 404 || c.name != "" {
		t.Errorf("fallback → handler=%q status=%d, want 미호출 404", c.name, rec.Code)
	}
	if gwerr.Tag(req) != "blocked_path" {
		t.Errorf("fallback tag=%q, want blocked_path", gwerr.Tag(req))
	}
}

func TestRequiresAuth(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/api/v1/me", true},
		{"/api/v1/me/nickname", true},
		{"/api/v1/mefoo", false},
		{"/api/v1/median", false},
		{"/api/v1/themes", false},
		{"/api/v1/auth/login", false},
		{"/healthz", false},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		if got := RequiresAuth(req); got != tc.want {
			t.Errorf("RequiresAuth(%s) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         middleware.Class
	}{
		{"POST", "/api/v1/auth/login", middleware.ClassAuth},
		{"POST", "/api/v1/auth/signup", middleware.ClassAuth},
		{"POST", "/api/v1/auth/refresh", middleware.ClassAuth},
		{"GET", "/api/v1/auth/something", middleware.ClassGeneral},
		{"POST", "/api/v1/themes", middleware.ClassGeneral},
		{"GET", "/api/v1/themes", middleware.ClassGeneral},
		{"GET", "/kg/api/v1/companies/005930", middleware.ClassGeneral},
		{"GET", "/healthz", middleware.ClassNone},
		{"GET", "/actuator/health", middleware.ClassGeneral},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if got := Classify(req); got != tc.want {
			t.Errorf("Classify(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
