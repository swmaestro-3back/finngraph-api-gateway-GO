package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

func TestBodyLimit_RejectsOversizedContentLength(t *testing.T) {
	var reached bool
	h := BodyLimit(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))

	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(make([]byte, 2048)))
	req = gwerr.WithTag(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 413 {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"PAYLOAD_TOO_LARGE"`)) {
		t.Errorf("body = %s, want PAYLOAD_TOO_LARGE envelope", rec.Body.String())
	}
	if reached {
		t.Error("한도 초과 요청이 다음 핸들러에 도달")
	}
	if gwerr.Tag(req) != "blocked_path" {
		t.Errorf("tag = %q, want blocked_path", gwerr.Tag(req))
	}
}

func TestBodyLimit_PassesSmallBody(t *testing.T) {
	var got string
	h := BodyLimit(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 16)
		n, _ := r.Body.Read(b)
		got = string(b[:n])
	}))

	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"a":1}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got != `{"a":1}` {
		t.Errorf("바디 = %q, want 원문", got)
	}
}

func TestBodyLimit_CapsChunkedBodyMidRead(t *testing.T) {
	var readErr error
	h := BodyLimit(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = new(bytes.Buffer).ReadFrom(r.Body)
	}))

	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(make([]byte, 4096)))
	req.ContentLength = -1
	h.ServeHTTP(httptest.NewRecorder(), req)

	var mbe *http.MaxBytesError
	if !errorsAs(readErr, &mbe) {
		t.Errorf("chunked 초과 읽기 err = %v, want MaxBytesError", readErr)
	}
}

func errorsAs(err error, target any) bool {
	if err == nil {
		return false
	}
	switch t := target.(type) {
	case **http.MaxBytesError:
		e, ok := err.(*http.MaxBytesError)
		if ok {
			*t = e
		}
		return ok
	}
	return false
}
