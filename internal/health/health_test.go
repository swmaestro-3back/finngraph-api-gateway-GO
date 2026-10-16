package health

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHandler_Returns200OK(t *testing.T) {
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body 파싱 실패: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf(`body = %v, want {"status":"ok"}`, body)
	}
}

func TestHandler_DrainReturns503(t *testing.T) {
	h := New()
	h.Drain()

	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))

	if rec.Code != 503 {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body 파싱 실패: %v", err)
	}
	if body["status"] != "draining" {
		t.Errorf(`body = %v, want {"status":"draining"}`, body)
	}
}
