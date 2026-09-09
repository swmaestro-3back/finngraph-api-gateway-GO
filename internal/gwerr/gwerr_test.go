package gwerr

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWrite_EnvelopeShape(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, 503, "UPSTREAM_TIMEOUT", "서버 응답이 지연되고 있습니다.")

	if rec.Code != 503 {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body JSON 파싱 실패: %v (body=%q)", err, rec.Body.String())
	}
	if got.Error.Code != "UPSTREAM_TIMEOUT" {
		t.Errorf("error.code = %q, want UPSTREAM_TIMEOUT", got.Error.Code)
	}
	if got.Error.Message != "서버 응답이 지연되고 있습니다." {
		t.Errorf("error.message = %q", got.Error.Message)
	}
}

func TestWrite_NoDetailsField(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, 404, "NOT_FOUND", "요청한 경로를 찾을 수 없습니다.")

	var raw map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body JSON 파싱 실패: %v", err)
	}
	if _, ok := raw["error"]["details"]; ok {
		t.Error("details 필드는 게이트웨이 봉투에 포함되면 안 됨 (Design §6.2)")
	}
}
