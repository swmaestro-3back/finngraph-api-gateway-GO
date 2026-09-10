package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/finngraph/finngraph-api-gateway/internal/gwerr"
)

func captureLog(t *testing.T, inner http.HandlerFunc, req *http.Request) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	chain := RequestID()(RealIP(true)(AccessLog(logger)(inner)))
	chain.ServeHTTP(httptest.NewRecorder(), req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("로그 JSON 파싱 실패: %v (raw=%q)", err, buf.String())
	}
	return entry
}

func TestAccessLog_FieldsOnSuccess(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/themes?page=0", nil)
	req.RemoteAddr = "172.18.0.5:41234"
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req.Header.Set("X-Request-Id", "req-42")

	entry := captureLog(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}, req)

	if entry["request_id"] != "req-42" {
		t.Errorf("request_id = %v, want req-42", entry["request_id"])
	}
	if entry["client_ip"] != "203.0.113.9" {
		t.Errorf("client_ip = %v, want 203.0.113.9", entry["client_ip"])
	}
	if entry["method"] != "GET" {
		t.Errorf("method = %v, want GET", entry["method"])
	}
	if entry["path"] != "/api/v1/themes" {
		t.Errorf("path = %v, want /api/v1/themes", entry["path"])
	}
	if entry["status"] != float64(200) {
		t.Errorf("status = %v, want 200 (암묵 WriteHeader)", entry["status"])
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Error("duration_ms 필드 없음")
	}
	if entry["err"] != "" {
		t.Errorf("err = %v, want 빈 문자열", entry["err"])
	}
}

func TestAccessLog_ExplicitStatusAndKoreanPath(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/themes/%EC%9D%B4%EC%B0%A8%EC%A0%84%EC%A7%80", nil)

	entry := captureLog(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}, req)

	if entry["status"] != float64(404) {
		t.Errorf("status = %v, want 404", entry["status"])
	}
	if entry["path"] != "/api/v1/themes/%EC%9D%B4%EC%B0%A8%EC%A0%84%EC%A7%80" {
		t.Errorf("path = %v, want 인코딩 원문", entry["path"])
	}
}

func TestAccessLog_ErrTagFromDownstream(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/themes", nil)

	entry := captureLog(t, func(w http.ResponseWriter, r *http.Request) {
		gwerr.SetTag(r, "rate_limited")
		w.WriteHeader(429)
	}, req)

	if entry["err"] != "rate_limited" {
		t.Errorf("err = %v, want rate_limited", entry["err"])
	}
}
