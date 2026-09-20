package middleware

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

func testKeyPair(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mintES256(t *testing.T, key *ecdsa.PrivateKey, sub string, exp time.Time) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		Subject:   sub,
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodES256, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func requiresMe(r *http.Request) bool {
	p := r.URL.EscapedPath()
	return p == "/api/v1/me" || strings.HasPrefix(p, "/api/v1/me/")
}

func jwtServe(t *testing.T, key *ecdsa.PublicKey, path, authHeader string) (*httptest.ResponseRecorder, *http.Request, *bool) {
	t.Helper()
	reached := false
	var gotAuth string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		gotAuth = r.Header.Get("Authorization")
		_ = gotAuth
		w.WriteHeader(200)
	})
	req := httptest.NewRequest("GET", path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	req = gwerr.WithTag(req)
	rec := httptest.NewRecorder()
	JWTAuth(key, requiresMe)(inner).ServeHTTP(rec, req)
	return rec, req, &reached
}

func TestJWTAuth_NilKeyPassesThrough(t *testing.T) {
	rec, _, reached := jwtServe(t, nil, "/api/v1/me", "")
	if rec.Code != 200 || !*reached {
		t.Errorf("nil 키: status=%d reached=%v, want 200 통과", rec.Code, *reached)
	}
}

func TestJWTAuth_UnprotectedPathIgnored(t *testing.T) {
	key := testKeyPair(t)
	rec, _, reached := jwtServe(t, &key.PublicKey, "/api/v1/themes", "Bearer garbage")
	if rec.Code != 200 || !*reached {
		t.Errorf("비보호 경로: status=%d reached=%v, want 200 통과", rec.Code, *reached)
	}
}

func TestJWTAuth_MissingTokenRejected(t *testing.T) {
	key := testKeyPair(t)
	for name, header := range map[string]string{
		"헤더 없음":    "",
		"Basic":    "Basic dXNlcjpwdw==",
		"빈 Bearer": "Bearer   ",
	} {
		t.Run(name, func(t *testing.T) {
			rec, req, reached := jwtServe(t, &key.PublicKey, "/api/v1/me", header)
			if rec.Code != 401 || *reached {
				t.Errorf("status=%d reached=%v, want 401 미도달", rec.Code, *reached)
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte(`"UNAUTHORIZED"`)) ||
				!bytes.Contains(rec.Body.Bytes(), []byte("인증이 필요합니다.")) {
				t.Errorf("body = %s", rec.Body.String())
			}
			if gwerr.Tag(req) != "jwt_missing" {
				t.Errorf("tag = %q, want jwt_missing", gwerr.Tag(req))
			}
		})
	}
}

func TestJWTAuth_ValidTokenPassesWithHeaderIntact(t *testing.T) {
	key := testKeyPair(t)
	token := mintES256(t, key, "42", time.Now().Add(30*time.Minute))

	var gotAuth string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	})
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	JWTAuth(&key.PublicKey, requiresMe)(inner).ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotAuth != "Bearer "+token {
		t.Error("Authorization 헤더가 원본과 다름 — backend 이중 검증이 소비해야 함")
	}
}

func TestJWTAuth_ExpiredTokenReturnsTokenExpired(t *testing.T) {
	key := testKeyPair(t)
	token := mintES256(t, key, "42", time.Now().Add(-time.Minute))

	rec, req, reached := jwtServe(t, &key.PublicKey, "/api/v1/me", "Bearer "+token)
	if rec.Code != 401 || *reached {
		t.Fatalf("status=%d reached=%v, want 401 미도달", rec.Code, *reached)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"TOKEN_EXPIRED"`)) ||
		!bytes.Contains(rec.Body.Bytes(), []byte("access토큰이 만료되었습니다.")) {
		t.Errorf("body = %s, want TOKEN_EXPIRED 봉투", rec.Body.String())
	}
	if gwerr.Tag(req) != "jwt_expired" {
		t.Errorf("tag = %q, want jwt_expired", gwerr.Tag(req))
	}
}

func TestJWTAuth_InvalidTokensRejected(t *testing.T) {
	key := testKeyPair(t)
	other := testKeyPair(t)

	pubDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	hsConfusion, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: "42", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString(pubDER)
	if err != nil {
		t.Fatal(err)
	}
	noneToken, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Subject: "42", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	noExp, err := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
		Subject: "42",
	}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"타 키 서명":           mintES256(t, other, "42", time.Now().Add(time.Hour)),
		"HS256 혼동 공격":      hsConfusion,
		"alg=none":         noneToken,
		"exp 부재":           noExp,
		"sub 비숫자":          mintES256(t, key, "not-a-number", time.Now().Add(time.Hour)),
		"쓰레기":              "garbage.garbage.garbage",
		"만료+타키 서명 (서명 우선)": mintES256(t, other, "42", time.Now().Add(-time.Hour)),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			rec, req, reached := jwtServe(t, &key.PublicKey, "/api/v1/me", "Bearer "+token)
			if rec.Code != 401 || *reached {
				t.Errorf("status=%d reached=%v, want 401 미도달", rec.Code, *reached)
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte(`"UNAUTHORIZED"`)) {
				t.Errorf("body = %s, want UNAUTHORIZED", rec.Body.String())
			}
			if gwerr.Tag(req) != "jwt_invalid" {
				t.Errorf("tag = %q, want jwt_invalid", gwerr.Tag(req))
			}
		})
	}
}
