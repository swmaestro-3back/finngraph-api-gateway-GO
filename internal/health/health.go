package health

import (
	"net/http"
	"sync/atomic"
)

type Health struct {
	draining atomic.Bool
}

func New() *Health {
	return &Health{}
}

func (h *Health) Drain() {
	h.draining.Store(true)
}

func (h *Health) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if h.draining.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"draining"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
}
