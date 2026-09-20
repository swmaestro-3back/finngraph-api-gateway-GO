package router

import (
	"net/http"
	"strings"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/middleware"
)

func New(backend, kg, healthz http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hasDotSegment(r.URL.Path) {
			blocked(w, r)
			return
		}
		p := r.URL.EscapedPath()
		switch {
		case p == "/healthz":
			healthz.ServeHTTP(w, r)
		case p == "/actuator" || strings.HasPrefix(p, "/actuator/"):
			blocked(w, r)
		case strings.HasPrefix(p, "/kg/"):
			if r.Method != http.MethodGet {
				gwerr.SetTag(r, "blocked_path")
				w.Header().Set("Allow", http.MethodGet)
				gwerr.Write(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
					"허용되지 않은 메서드입니다.")
				return
			}
			kg.ServeHTTP(w, r)
		case strings.HasPrefix(p, "/api/"):
			backend.ServeHTTP(w, r)
		default:
			blocked(w, r)
		}
	})
}

func RequiresAuth(r *http.Request) bool {
	p := r.URL.EscapedPath()
	return p == "/api/v1/me" || strings.HasPrefix(p, "/api/v1/me/")
}

func Classify(r *http.Request) middleware.Class {
	p := r.URL.EscapedPath()
	switch {
	case p == "/healthz":
		return middleware.ClassNone
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/api/v1/auth/"):
		return middleware.ClassAuth
	default:
		return middleware.ClassGeneral
	}
}

func hasDotSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}

func blocked(w http.ResponseWriter, r *http.Request) {
	gwerr.SetTag(r, "blocked_path")
	gwerr.Write(w, http.StatusNotFound, "NOT_FOUND", "요청한 경로를 찾을 수 없습니다.")
}
