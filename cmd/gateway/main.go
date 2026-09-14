package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/config"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/health"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/middleware"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/proxy"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/router"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "self liveness probe")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	if *healthcheck {
		if err := runHealthcheck(probeURL(cfg.ListenAddr)); err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck:", err)
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           buildHandler(cfg, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      12 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("gateway listening", "addr", cfg.ListenAddr,
			"backend", cfg.BackendURL.String(), "kg", cfg.KGURL.String())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "err", err.Error())
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown incomplete", "err", err.Error())
	}
}

func buildHandler(cfg *config.Config, logger *slog.Logger) http.Handler {
	tr := proxy.NewTransport(cfg)
	rt := router.New(
		proxy.New(cfg.BackendURL, tr, logger, middleware.ClientIP),
		proxy.NewKG(cfg.KGURL, tr, logger, middleware.ClientIP),
		health.Handler(),
	)

	return middleware.Recover(logger)(
		middleware.RealIP(cfg.TrustProxy, cfg.ClientIPSource)(
			middleware.RequestID()(
				middleware.AccessLog(logger)(
					middleware.BodyLimit(1 << 20)(
						middleware.RateLimit(cfg, router.Classify)(
							middleware.Deadline(cfg.UpstreamTimeout)(rt)))))))
}

func probeURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "http://127.0.0.1:8080/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s/healthz", net.JoinHostPort(host, port))
}

func runHealthcheck(url string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
