package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeadline_SetsContextDeadline(t *testing.T) {
	var dl time.Time
	var ok bool
	h := Deadline(500 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dl, ok = r.Context().Deadline()
	}))

	start := time.Now()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	if !ok {
		t.Fatal("context에 데드라인 없음")
	}
	remaining := time.Until(dl)
	_ = start
	if remaining <= 0 || remaining > 500*time.Millisecond {
		t.Errorf("데드라인 여유 = %v, want 0 < d <= 500ms", remaining)
	}
}

func TestDeadline_CancelsAfterExpiry(t *testing.T) {
	var err error
	h := Deadline(30 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		err = r.Context().Err()
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	if err == nil {
		t.Fatal("만료 후 ctx.Err() = nil, want DeadlineExceeded")
	}
}
