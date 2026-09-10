package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func clientIPSeen(h func(http.Handler) http.Handler, req *http.Request) string {
	var got string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = ClientIP(r)
	})
	h(inner).ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestRealIP_TrustedHeaderWins(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "203.0.113.9")

	if got := clientIPSeen(RealIP(true), req); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, want X-Real-IP 값", got)
	}
}

func TestRealIP_NoHeaderFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"

	if got := clientIPSeen(RealIP(true), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want RemoteAddr host", got)
	}
}

func TestRealIP_UntrustedIgnoresHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "203.0.113.9")

	if got := clientIPSeen(RealIP(false), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want RemoteAddr host (헤더 무시)", got)
	}
}

func TestClientIP_WithoutMiddleware(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"

	if got := ClientIP(req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want RemoteAddr host 폴백", got)
	}
}
