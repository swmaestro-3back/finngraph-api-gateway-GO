package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecover_PanicReturns500Envelope(t *testing.T) {
	h := Recover(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/themes", nil))

	if rec.Code != 500 {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"INTERNAL"`)) {
		t.Errorf("body = %s, want INTERNAL code", rec.Body.String())
	}
}

func TestRecover_ErrAbortHandlerRepanics(t *testing.T) {
	h := Recover(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recover() = %v, want http.ErrAbortHandler re-panic", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	t.Error("panic이 전파되지 않음")
}

func TestRecover_NormalRequestPassesThrough(t *testing.T) {
	h := Recover(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		io.WriteString(w, "ok")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != 201 || rec.Body.String() != "ok" {
		t.Errorf("got %d %q, want 201 ok", rec.Code, rec.Body.String())
	}
}
