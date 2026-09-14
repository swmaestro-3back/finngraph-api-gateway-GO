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

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, want X-Real-IP 값", got)
	}
}

func TestRealIP_NoHeaderFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want RemoteAddr host", got)
	}
}

func TestRealIP_UntrustedIgnoresHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "203.0.113.9")

	if got := clientIPSeen(RealIP(false, "auto"), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want RemoteAddr host (헤더 무시)", got)
	}
}

func TestRealIP_ViewerAddressWinsOverRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "130.176.0.10")
	req.Header.Set("CloudFront-Viewer-Address", "198.51.100.10:46532")

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "198.51.100.10" {
		t.Errorf("ClientIP = %q, want CloudFront-Viewer-Address의 IP", got)
	}
}

func TestRealIP_ViewerAddressIPv6(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("CloudFront-Viewer-Address", "2001:db8:85a3::8a2e:370:7334:39106")

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "2001:db8:85a3::8a2e:370:7334" {
		t.Errorf("ClientIP = %q, want 포트 제거된 IPv6", got)
	}
}

func TestRealIP_ViewerAddressMalformedFallsBackToRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req.Header.Set("CloudFront-Viewer-Address", "not-an-address")

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, want X-Real-IP 폴백", got)
	}
}

func TestRealIP_UntrustedIgnoresViewerAddress(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("CloudFront-Viewer-Address", "198.51.100.10:46532")

	if got := clientIPSeen(RealIP(false, "auto"), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want RemoteAddr host (헤더 무시)", got)
	}
}

func TestRealIP_XFFLastEntryWhenNoOtherHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "203.0.113.7" {
		t.Errorf("ClientIP = %q, want XFF 값", got)
	}
}

func TestRealIP_XFFSpoofedPrefixIgnored(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 8.8.8.8, 198.51.100.77")

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "198.51.100.77" {
		t.Errorf("ClientIP = %q, want 최후미 항목 (ALB가 append한 값만 신뢰)", got)
	}
}

func TestRealIP_XFFIPv6LastEntry(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 2001:db8::1")

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "2001:db8::1" {
		t.Errorf("ClientIP = %q, want IPv6 최후미", got)
	}
}

func TestRealIP_XRealIPWinsOverXFF(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-For", "198.51.100.77")

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, want X-Real-IP 우선", got)
	}
}

func TestRealIP_XFFMalformedFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Forwarded-For", "1.1.1.1, garbage")

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want RemoteAddr 폴백 (비IP 최후미 불신)", got)
	}
}

func TestRealIP_UntrustedIgnoresXFF(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Forwarded-For", "198.51.100.77")

	if got := clientIPSeen(RealIP(false, "auto"), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want RemoteAddr host (헤더 무시)", got)
	}
}

func TestRealIP_SourceXFF_IgnoresForgedHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("CloudFront-Viewer-Address", "6.6.6.6:1234")
	req.Header.Set("X-Real-IP", "7.7.7.7")
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 203.0.113.50")

	if got := clientIPSeen(RealIP(true, "xff"), req); got != "203.0.113.50" {
		t.Errorf("ClientIP = %q, want XFF 최후미 (위조 가능 헤더 2종 무시)", got)
	}
}

func TestRealIP_SourceXFF_DuplicateFieldsUseLastFieldLastEntry(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Add("X-Forwarded-For", "6.6.6.6, 7.7.7.7")
	req.Header.Add("X-Forwarded-For", "8.8.8.8, 203.0.113.80")

	if got := clientIPSeen(RealIP(true, "xff"), req); got != "203.0.113.80" {
		t.Errorf("ClientIP = %q, want 마지막 필드의 최후미 (첫 필드만 읽으면 우회됨)", got)
	}
}

func TestRealIP_AutoXFF_DuplicateFieldsUseLastFieldLastEntry(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Add("X-Forwarded-For", "6.6.6.6")
	req.Header.Add("X-Forwarded-For", "203.0.113.81")

	if got := clientIPSeen(RealIP(true, "auto"), req); got != "203.0.113.81" {
		t.Errorf("ClientIP = %q, want 마지막 필드 값", got)
	}
}

func TestRealIP_SourceXFF_AbsentFallsBackToSocket(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "7.7.7.7")

	if got := clientIPSeen(RealIP(true, "xff"), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want 소켓 폴백 (X-Real-IP로 새지 않음)", got)
	}
}

func TestRealIP_SourceCloudFront_OnlyViewerAddress(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "7.7.7.7")
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	if got := clientIPSeen(RealIP(true, "cloudfront"), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want 소켓 폴백 (타 헤더로 새지 않음)", got)
	}
}

func TestRealIP_SourceRealIP_OnlyThatHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-For", "9.9.9.9")

	if got := clientIPSeen(RealIP(true, "real-ip"), req); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, want X-Real-IP", got)
	}
}

func TestRealIP_SourceSocket_IgnoresAllHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("CloudFront-Viewer-Address", "6.6.6.6:1234")
	req.Header.Set("X-Real-IP", "7.7.7.7")
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	if got := clientIPSeen(RealIP(true, "socket"), req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want 소켓 주소 (전 헤더 무시)", got)
	}
}

func TestClientIP_WithoutMiddleware(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.5:41234"

	if got := ClientIP(req); got != "172.18.0.5" {
		t.Errorf("ClientIP = %q, want RemoteAddr host 폴백", got)
	}
}
