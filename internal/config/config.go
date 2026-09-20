package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	RedisAddr        string
	JWTPublicKey     *ecdsa.PublicKey
	CORSOrigins      []string
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
	cfg.RedisAddr = os.Getenv("GW_REDIS_ADDR")
	if cfg.JWTPublicKey, err = parseECPublicKey("GW_JWT_PUBLIC_KEY"); err != nil {
		return nil, err
	}
	if cfg.CORSOrigins, err = parseCORSOrigins("GW_CORS_ORIGINS"); err != nil {
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

func parseECPublicKey(key string) (*ecdsa.PublicKey, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return nil, nil
	}
	der, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: base64 디코딩 실패: %w", key, err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("%s: SPKI 파싱 실패: %w", key, err)
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s: EC 공개키 필요 (실제: %T)", key, parsed)
	}
	if pub.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%s: P-256 곡선 필요 (실제: %s)", key, pub.Curve.Params().Name)
	}
	return pub, nil
}

func parseCORSOrigins(key string) ([]string, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return nil, nil
	}
	var origins []string
	for _, item := range strings.Split(raw, ",") {
		origin := strings.ToLower(strings.TrimSpace(item))
		if origin == "" {
			return nil, fmt.Errorf("%s: 빈 origin 항목 %q", key, raw)
		}
		u, err := url.Parse(origin)
		if err != nil {
			return nil, fmt.Errorf("%s: origin 파싱 실패 %q: %w", key, origin, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("%s: http(s) 스킴 필요 %q", key, origin)
		}
		if u.Host == "" {
			return nil, fmt.Errorf("%s: 호스트 없음 %q", key, origin)
		}
		if u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return nil, fmt.Errorf("%s: origin은 scheme://host[:port] 형식만 허용 %q", key, origin)
		}
		origins = append(origins, origin)
	}
	return origins, nil
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
