package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/config"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

func NewTransport(cfg *config.Config) *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: cfg.DialTimeout}).DialContext,
		ResponseHeaderTimeout: cfg.UpstreamTimeout,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConnsPerHost:   32,
		MaxConnsPerHost:       cfg.MaxConnsPerHost,
		DisableCompression:    true,
	}
}

func New(target *url.URL, tr http.RoundTripper, logger *slog.Logger) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host
			for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
				if v, ok := pr.In.Header[h]; ok {
					pr.Out.Header[h] = v
				}
			}
		},
		Transport:     tr,
		FlushInterval: -1,
		ErrorHandler:  errorHandler(logger),
	}
}

func NewKG(target *url.URL, tr http.RoundTripper, logger *slog.Logger) http.Handler {
	return http.StripPrefix("/kg", New(target, tr, logger))
}

func errorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, context.Canceled) {
			logger.Info("client canceled", "path", r.URL.EscapedPath())
			return
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			logger.Warn("upstream timeout", "path", r.URL.EscapedPath(), "err", err.Error())
			gwerr.SetTag(r, "upstream_timeout")
			gwerr.Write(w, http.StatusServiceUnavailable, "UPSTREAM_TIMEOUT",
				"서버 응답이 지연되고 있습니다. 잠시 후 다시 시도해주세요.")
			return
		}
		logger.Warn("upstream unreachable", "path", r.URL.EscapedPath(), "err", err.Error())
		gwerr.SetTag(r, "upstream_unreachable")
		gwerr.Write(w, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE",
			"서버에 연결할 수 없습니다. 잠시 후 다시 시도해주세요.")
	}
}
