package middleware

import (
	"log/slog"
	"net/http"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.Error("panic recovered", "path", r.URL.EscapedPath(), "panic", v)
				gwerr.Write(w, http.StatusInternalServerError, "INTERNAL",
					"일시적인 오류가 발생했습니다. 잠시 후 다시 시도해주세요.")
			}()
			next.ServeHTTP(w, r)
		})
	}
}
