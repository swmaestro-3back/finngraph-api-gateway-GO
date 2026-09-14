package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with clean env: unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if got := cfg.BackendURL.String(); got != "http://finngraph-backend:8080" {
		t.Errorf("BackendURL = %q, want %q", got, "http://finngraph-backend:8080")
	}
	if got := cfg.KGURL.String(); got != "http://finngraph-ai-server:8000" {
		t.Errorf("KGURL = %q, want %q", got, "http://finngraph-ai-server:8000")
	}
	if cfg.UpstreamTimeout != 8*time.Second {
		t.Errorf("UpstreamTimeout = %v, want 8s", cfg.UpstreamTimeout)
	}
	if cfg.DialTimeout != 2*time.Second {
		t.Errorf("DialTimeout = %v, want 2s", cfg.DialTimeout)
	}
	if cfg.MaxConnsPerHost != 64 {
		t.Errorf("MaxConnsPerHost = %d, want 64", cfg.MaxConnsPerHost)
	}
	if cfg.RateGeneralRPS != 30 || cfg.RateGeneralBurst != 60 {
		t.Errorf("general rate = %v/%d, want 30/60", cfg.RateGeneralRPS, cfg.RateGeneralBurst)
	}
	if cfg.RateAuthRPM != 10 || cfg.RateAuthBurst != 10 {
		t.Errorf("auth rate = %v/%d, want 10/10", cfg.RateAuthRPM, cfg.RateAuthBurst)
	}
	if !cfg.TrustProxy {
		t.Error("TrustProxy = false, want true")
	}
	if cfg.ClientIPSource != "auto" {
		t.Errorf("ClientIPSource = %q, want auto", cfg.ClientIPSource)
	}
}

func TestLoad_ClientIPSource(t *testing.T) {
	for _, valid := range []string{"auto", "cloudfront", "real-ip", "xff", "socket"} {
		t.Run("valid_"+valid, func(t *testing.T) {
			t.Setenv("GW_CLIENT_IP_SOURCE", valid)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load(): %v", err)
			}
			if cfg.ClientIPSource != valid {
				t.Errorf("ClientIPSource = %q, want %q", cfg.ClientIPSource, valid)
			}
		})
	}
	t.Run("invalid_fails_closed", func(t *testing.T) {
		t.Setenv("GW_CLIENT_IP_SOURCE", "nginx")
		if _, err := Load(); err == nil {
			t.Error("want error for unknown source")
		}
	})
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("GW_LISTEN_ADDR", ":9999")
	t.Setenv("GW_BACKEND_URL", "http://localhost:8081")
	t.Setenv("GW_KG_URL", "http://localhost:8000")
	t.Setenv("GW_UPSTREAM_TIMEOUT", "3s")
	t.Setenv("GW_UPSTREAM_DIAL_TIMEOUT", "500ms")
	t.Setenv("GW_UPSTREAM_MAX_CONNS", "16")
	t.Setenv("GW_RATE_GENERAL_RPS", "100")
	t.Setenv("GW_RATE_GENERAL_BURST", "200")
	t.Setenv("GW_RATE_AUTH_RPM", "30")
	t.Setenv("GW_RATE_AUTH_BURST", "5")
	t.Setenv("GW_TRUST_PROXY", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with overrides: unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":9999" {
		t.Errorf("ListenAddr = %q, want :9999", cfg.ListenAddr)
	}
	if got := cfg.BackendURL.String(); got != "http://localhost:8081" {
		t.Errorf("BackendURL = %q, want http://localhost:8081", got)
	}
	if cfg.UpstreamTimeout != 3*time.Second {
		t.Errorf("UpstreamTimeout = %v, want 3s", cfg.UpstreamTimeout)
	}
	if cfg.DialTimeout != 500*time.Millisecond {
		t.Errorf("DialTimeout = %v, want 500ms", cfg.DialTimeout)
	}
	if cfg.MaxConnsPerHost != 16 {
		t.Errorf("MaxConnsPerHost = %d, want 16", cfg.MaxConnsPerHost)
	}
	if cfg.RateGeneralRPS != 100 || cfg.RateGeneralBurst != 200 {
		t.Errorf("general rate = %v/%d, want 100/200", cfg.RateGeneralRPS, cfg.RateGeneralBurst)
	}
	if cfg.RateAuthRPM != 30 || cfg.RateAuthBurst != 5 {
		t.Errorf("auth rate = %v/%d, want 30/5", cfg.RateAuthRPM, cfg.RateAuthBurst)
	}
	if cfg.TrustProxy {
		t.Error("TrustProxy = true, want false")
	}
}

func TestLoad_FailClosed(t *testing.T) {
	cases := []struct{ key, val string }{
		{"GW_BACKEND_URL", "://not-a-url"},
		{"GW_BACKEND_URL", "finngraph-backend:8080"},
		{"GW_KG_URL", "://bad"},
		{"GW_UPSTREAM_TIMEOUT", "eight seconds"},
		{"GW_UPSTREAM_TIMEOUT", "-1s"},
		{"GW_UPSTREAM_DIAL_TIMEOUT", "0"},
		{"GW_UPSTREAM_MAX_CONNS", "0"},
		{"GW_UPSTREAM_MAX_CONNS", "-5"},
		{"GW_RATE_GENERAL_RPS", "-1"},
		{"GW_RATE_GENERAL_BURST", "0"},
		{"GW_RATE_AUTH_RPM", "abc"},
		{"GW_TRUST_PROXY", "maybe"},
	}
	for _, c := range cases {
		t.Run(c.key+"="+c.val, func(t *testing.T) {
			t.Setenv(c.key, c.val)
			if _, err := Load(); err == nil {
				t.Errorf("Load() with %s=%q: want error, got nil", c.key, c.val)
			}
		})
	}
}
