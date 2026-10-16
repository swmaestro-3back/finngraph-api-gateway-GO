package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestRequestID_AcceptsInbound(t *testing.T) {
	var seenHeader, seenCtx string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeader = r.Header.Get("X-Request-Id")
		seenCtx = GetRequestID(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-Id", "nginx-abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seenHeader != "nginx-abc-123" {
		t.Errorf("업스트림 전파 헤더 = %q, want 인바운드 값", seenHeader)
	}
	if seenCtx != "nginx-abc-123" {
		t.Errorf("GetRequestID = %q, want 인바운드 값", seenCtx)
	}
	if got := rec.Header().Get("X-Request-Id"); got != "nginx-abc-123" {
		t.Errorf("응답 에코 = %q, want 인바운드 값", got)
	}
}

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	var seenHeader string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeader = r.Header.Get("X-Request-Id")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	if !hex32.MatchString(seenHeader) {
		t.Errorf("생성된 ID = %q, want 32자 hex", seenHeader)
	}
	if echo := rec.Header().Get("X-Request-Id"); echo != seenHeader {
		t.Errorf("응답 에코 %q != 전파 헤더 %q", echo, seenHeader)
	}
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	ids := map[string]bool{}
	for range 10 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		ids[rec.Header().Get("X-Request-Id")] = true
	}
	if len(ids) != 10 {
		t.Errorf("고유 ID %d개, want 10", len(ids))
	}
}
