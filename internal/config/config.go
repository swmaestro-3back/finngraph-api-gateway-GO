package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr       string
	BackendURL       *url.URL
	KGURL            *url.URL
	UpstreamTimeout  time.Duration
	DialTimeout      time.Duration
	MaxConnsPerHost  int
	RateGeneralRPS   float64
	RateGeneralBurst int
	RateAuthRPM      float64
	RateAuthBurst    int
	TrustProxy       bool
	ClientIPSource   string
	LogLevel         slog.Level
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr: getenv("GW_LISTEN_ADDR", ":8080"),
	}

	var err error
	if cfg.BackendURL, err = parseURL("GW_BACKEND_URL", "http://finngraph-backend:8080"); err != nil {
		return nil, err
	}
	if cfg.KGURL, err = parseURL("GW_KG_URL", "http://finngraph-ai-server:8000"); err != nil {
		return nil, err
	}
	if cfg.UpstreamTimeout, err = parseDuration("GW_UPSTREAM_TIMEOUT", 8*time.Second); err != nil {
		return nil, err
	}
	if cfg.DialTimeout, err = parseDuration("GW_UPSTREAM_DIAL_TIMEOUT", 2*time.Second); err != nil {
		return nil, err
	}
	if cfg.MaxConnsPerHost, err = parsePositiveInt("GW_UPSTREAM_MAX_CONNS", 64); err != nil {
		return nil, err
	}
	if cfg.RateGeneralRPS, err = parsePositiveFloat("GW_RATE_GENERAL_RPS", 30); err != nil {
		return nil, err
	}
	if cfg.RateGeneralBurst, err = parsePositiveInt("GW_RATE_GENERAL_BURST", 60); err != nil {
		return nil, err
	}
	if cfg.RateAuthRPM, err = parsePositiveFloat("GW_RATE_AUTH_RPM", 10); err != nil {
		return nil, err
	}
	if cfg.RateAuthBurst, err = parsePositiveInt("GW_RATE_AUTH_BURST", 10); err != nil {
		return nil, err
	}
	if cfg.TrustProxy, err = parseBool("GW_TRUST_PROXY", true); err != nil {
		return nil, err
	}
	if cfg.ClientIPSource, err = parseEnum("GW_CLIENT_IP_SOURCE", "auto",
		[]string{"auto", "cloudfront", "real-ip", "xff", "socket"}); err != nil {
		return nil, err
	}
	if cfg.LogLevel, err = parseLogLevel("GW_LOG_LEVEL", slog.LevelInfo); err != nil {
		return nil, err
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseURL(key, fallback string) (*url.URL, error) {
	raw := getenv(key, fallback)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: URL 파싱 실패 %q: %w", key, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%s: http(s) 스킴 필요 %q", key, raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%s: 호스트 없음 %q", key, raw)
	}
	return u, nil
}

func parseDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: duration 파싱 실패 %q: %w", key, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: 양수 duration 필요 %q", key, raw)
	}
	return d, nil
}

func parsePositiveInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: 정수 파싱 실패 %q: %w", key, raw, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s: 양수 필요 %q", key, raw)
	}
	return n, nil
}

func parsePositiveFloat(key string, fallback float64) (float64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: 실수 파싱 실패 %q: %w", key, raw, err)
	}
	if f <= 0 {
		return 0, fmt.Errorf("%s: 양수 필요 %q", key, raw)
	}
	return f, nil
}

func parseEnum(key, fallback string, allowed []string) (string, error) {
	raw := getenv(key, fallback)
	for _, a := range allowed {
		if raw == a {
			return raw, nil
		}
	}
	return "", fmt.Errorf("%s: 허용되지 않은 값 %q (허용: %v)", key, raw, allowed)
}

func parseBool(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: bool 파싱 실패 %q: %w", key, raw, err)
	}
	return b, nil
}

func parseLogLevel(key string, fallback slog.Level) (slog.Level, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(raw)); err != nil {
		return 0, fmt.Errorf("%s: 로그 레벨 파싱 실패 %q: %w", key, raw, err)
	}
	return lvl, nil
}
