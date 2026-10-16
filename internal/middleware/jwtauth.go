package middleware

import (
	"crypto/ecdsa"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

func JWTAuth(key *ecdsa.PublicKey, requiresAuth func(*http.Request) bool) func(http.Handler) http.Handler {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"ES256"}), jwt.WithExpirationRequired())
	keyfunc := func(*jwt.Token) (any, error) { return key, nil }

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key == nil || !requiresAuth(r) {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				rejectAuth(w, r, "jwt_missing", "UNAUTHORIZED", "인증이 필요합니다.")
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			if token == "" {
				rejectAuth(w, r, "jwt_missing", "UNAUTHORIZED", "인증이 필요합니다.")
				return
			}

			claims := &jwt.RegisteredClaims{}
			if _, err := parser.ParseWithClaims(token, claims, keyfunc); err != nil {
				if errors.Is(err, jwt.ErrTokenExpired) {
					rejectAuth(w, r, "jwt_expired", "TOKEN_EXPIRED", "access토큰이 만료되었습니다.")
					return
				}
				rejectAuth(w, r, "jwt_invalid", "UNAUTHORIZED", "인증이 필요합니다.")
				return
			}
			if _, err := strconv.ParseInt(claims.Subject, 10, 64); err != nil {
				rejectAuth(w, r, "jwt_invalid", "UNAUTHORIZED", "인증이 필요합니다.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func rejectAuth(w http.ResponseWriter, r *http.Request, tag, code, message string) {
	gwerr.SetTag(r, tag)
	gwerr.Write(w, http.StatusUnauthorized, code, message)
}
