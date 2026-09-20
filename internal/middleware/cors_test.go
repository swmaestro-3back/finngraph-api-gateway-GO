package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

const corsAllowed = "https://app.example.com"

func corsServe(t *testing.T, origins []string, method, origin, acrm string) (*httptest.ResponseRecorder, *http.Request, *bool) {
	t.Helper()
	reached := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(200)
	})
	req := httptest.NewRequest(method, "/api/v1/themes", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if acrm != "" {
		req.Header.Set("Access-Control-Request-Method", acrm)
	}
	req = gwerr.WithTag(req)
	rec := httptest.NewRecorder()
	CORS(origins)(inner).ServeHTTP(rec, req)
	return rec, req, &reached
}

func TestCORS_EmptyOriginsIsIdentity(t *testing.T) {
	rec, _, reached := corsServe(t, nil, "GET", corsAllowed, "")
	if !*reached || rec.Code != 200 {
		t.Errorf("reached=%v status=%d, want 통과", *reached, rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "" {
		t.Errorf("빈 origins인데 헤더 부착됨: %v", rec.Header())
	}
}

func TestCORS_NoOriginHeaderPassesThrough(t *testing.T) {
	rec, _, reached := corsServe(t, []string{corsAllowed}, "GET", "", "")
	if !*reached || rec.Code != 200 {
		t.Errorf("reached=%v status=%d, want 통과", *reached, rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "" {
		t.Errorf("Origin 없는데 헤더 부착됨: %v", rec.Header())
	}
}

func TestCORS_AllowedActualRequest(t *testing.T) {
	rec, req, reached := corsServe(t, []string{corsAllowed}, "GET", corsAllowed, "")
	if !*reached {
		t.Fatal("next 미도달")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != corsAllowed {
		t.Errorf("Allow-Origin = %q, want %q", got, corsAllowed)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("Allow-Credentials 부재")
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Errorf("Vary = %q, want Origin", rec.Header().Get("Vary"))
	}
	if gwerr.Tag(req) != "" {
		t.Errorf("tag = %q, want 없음", gwerr.Tag(req))
	}
}

func TestCORS_DeniedActualRequest(t *testing.T) {
	for _, origin := range []string{
		"https://evil.example",
		"https://evil-app.example.com",
		"https://app.example.com.evil.com",
	} {
		t.Run(origin, func(t *testing.T) {
			rec, req, reached := corsServe(t, []string{corsAllowed}, "GET", origin, "")
			if !*reached {
				t.Fatal("비허용 실요청은 통과해야 함 (차단은 브라우저 몫)")
			}
			if rec.Header().Get("Access-Control-Allow-Origin") != "" ||
				rec.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Errorf("비허용인데 Allow-* 부착: %v", rec.Header())
			}
			if rec.Header().Get("Vary") != "Origin" {
				t.Errorf("Vary = %q, want Origin", rec.Header().Get("Vary"))
			}
			if gwerr.Tag(req) != "cors_denied" {
				t.Errorf("tag = %q, want cors_denied", gwerr.Tag(req))
			}
		})
	}
}

func TestCORS_AllowedPreflight(t *testing.T) {
	rec, _, reached := corsServe(t, []string{corsAllowed}, "OPTIONS", corsAllowed, "POST")
	if *reached {
		t.Fatal("preflight가 업스트림에 도달함")
	}
	if rec.Code != 204 {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	for header, want := range map[string]string{
		"Access-Control-Allow-Origin":      corsAllowed,
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Methods":     "GET, POST, PUT, DELETE, PATCH",
		"Access-Control-Allow-Headers":     "Authorization, Content-Type",
		"Access-Control-Max-Age":           "600",
		"Vary":                             "Origin, Access-Control-Request-Method, Access-Control-Request-Headers",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestCORS_DeniedPreflight(t *testing.T) {
	rec, req, reached := corsServe(t, []string{corsAllowed}, "OPTIONS", "https://evil.example", "POST")
	if *reached {
		t.Fatal("비허용 preflight가 업스트림에 도달함")
	}
	if rec.Code != 204 {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("비허용인데 Allow-Origin 부착: %v", rec.Header())
	}
	if gwerr.Tag(req) != "cors_denied" {
		t.Errorf("tag = %q, want cors_denied", gwerr.Tag(req))
	}
}

func TestCORS_PlainOptionsWithoutOriginProxied(t *testing.T) {
	_, _, reached := corsServe(t, []string{corsAllowed}, "OPTIONS", "", "POST")
	if !*reached {
		t.Error("Origin 없는 OPTIONS는 기존대로 프록시돼야 함")
	}
}

func TestCORS_OptionsWithoutACRMTreatedAsActual(t *testing.T) {
	rec, _, reached := corsServe(t, []string{corsAllowed}, "OPTIONS", corsAllowed, "")
	if !*reached {
		t.Error("ACRM 없는 OPTIONS는 실요청 취급으로 통과해야 함")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != corsAllowed {
		t.Errorf("Allow-Origin = %q, want %q", got, corsAllowed)
	}
}

func TestCORS_OriginComparisonCaseInsensitive(t *testing.T) {
	rec, _, reached := corsServe(t, []string{corsAllowed}, "GET", "https://APP.example.com", "")
	if !*reached {
		t.Fatal("next 미도달")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://APP.example.com" {
		t.Errorf("Allow-Origin = %q, want 요청 origin 원문 반사", got)
	}
}
