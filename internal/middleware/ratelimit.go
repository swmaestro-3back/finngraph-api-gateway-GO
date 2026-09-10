package middleware

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/config"
	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

type Class int

const (
	ClassNone Class = iota
	ClassGeneral
	ClassAuth
)

type poolEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

type pool struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	entries map[string]*poolEntry
}

func newPool(limit rate.Limit, burst int) *pool {
	return &pool{limit: limit, burst: burst, entries: map[string]*poolEntry{}}
}

func (p *pool) get(ip string) *rate.Limiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[ip]
	if !ok {
		e = &poolEntry{lim: rate.NewLimiter(p.limit, p.burst)}
		p.entries[ip] = e
	}
	e.lastSeen = time.Now()
	return e.lim
}

func (p *pool) sweep(idle time.Duration) {
	cutoff := time.Now().Add(-idle)
	p.mu.Lock()
	defer p.mu.Unlock()
	for ip, e := range p.entries {
		if e.lastSeen.Before(cutoff) {
			delete(p.entries, ip)
		}
	}
}

func (p *pool) janitor(interval, idle time.Duration) {
	t := time.NewTicker(interval)
	for range t.C {
		p.sweep(idle)
	}
}

func RateLimit(cfg *config.Config, classify func(*http.Request) Class) func(http.Handler) http.Handler {
	general := newPool(rate.Limit(cfg.RateGeneralRPS), cfg.RateGeneralBurst)
	auth := newPool(rate.Limit(cfg.RateAuthRPM/60), cfg.RateAuthBurst)
	go general.janitor(time.Minute, 10*time.Minute)
	go auth.janitor(time.Minute, 10*time.Minute)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p *pool
			switch classify(r) {
			case ClassGeneral:
				p = general
			case ClassAuth:
				p = auth
			default:
				next.ServeHTTP(w, r)
				return
			}

			res := p.get(ClientIP(r)).Reserve()
			if !res.OK() {
				reject(w, r, 1)
				return
			}
			if delay := res.Delay(); delay > 0 {
				res.Cancel()
				reject(w, r, int(math.Ceil(delay.Seconds())))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func reject(w http.ResponseWriter, r *http.Request, retryAfter int) {
	if retryAfter < 1 {
		retryAfter = 1
	}
	gwerr.SetTag(r, "rate_limited")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	gwerr.Write(w, http.StatusTooManyRequests, "RATE_LIMITED",
		"요청이 너무 많습니다. 잠시 후에 다시 시도해주세요.")
}
