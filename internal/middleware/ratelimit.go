package middleware

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
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

type allower interface {
	allow(ip string) (bool, int)
}

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

func (p *pool) allow(ip string) (bool, int) {
	res := p.get(ip).Reserve()
	if !res.OK() {
		return false, 1
	}
	if delay := res.Delay(); delay > 0 {
		res.Cancel()
		return false, int(math.Ceil(delay.Seconds()))
	}
	return true, 0
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

var tokenBucketScript = redis.NewScript(`
local data = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil then
  tokens = burst
  ts = now
end
tokens = math.min(burst, tokens + math.max(0, now - ts) / 1000000 * rate)
local allowed = 0
local retry = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  retry = math.ceil((1 - tokens) / rate)
end
redis.call('HSET', KEYS[1], 'tokens', tokens, 'ts', now)
redis.call('EXPIRE', KEYS[1], 600)
return {allowed, retry}
`)

type redisPool struct {
	client *redis.Client
	prefix string
	rate   float64
	burst  int
	logger *slog.Logger
}

func (p *redisPool) allow(ip string) (bool, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := tokenBucketScript.Run(ctx, p.client, []string{p.prefix + ip},
		p.rate, p.burst, time.Now().UnixMicro()).Int64Slice()
	if err != nil || len(res) != 2 {
		p.logger.Warn("rate limit fail-open", "prefix", p.prefix, "err", errString(err))
		return true, 0
	}
	return res[0] == 1, int(res[1])
}

func errString(err error) string {
	if err == nil {
		return "malformed script result"
	}
	return err.Error()
}

func RateLimit(cfg *config.Config, logger *slog.Logger, classify func(*http.Request) Class) func(http.Handler) http.Handler {
	var general, auth allower
	if cfg.RedisAddr != "" {
		client := redis.NewClient(&redis.Options{
			Addr:         cfg.RedisAddr,
			DialTimeout:  200 * time.Millisecond,
			ReadTimeout:  200 * time.Millisecond,
			WriteTimeout: 200 * time.Millisecond,
		})
		general = &redisPool{client: client, prefix: "gw:rl:general:", rate: cfg.RateGeneralRPS, burst: cfg.RateGeneralBurst, logger: logger}
		auth = &redisPool{client: client, prefix: "gw:rl:auth:", rate: cfg.RateAuthRPM / 60, burst: cfg.RateAuthBurst, logger: logger}
	} else {
		gp := newPool(rate.Limit(cfg.RateGeneralRPS), cfg.RateGeneralBurst)
		ap := newPool(rate.Limit(cfg.RateAuthRPM/60), cfg.RateAuthBurst)
		go gp.janitor(time.Minute, 10*time.Minute)
		go ap.janitor(time.Minute, 10*time.Minute)
		general, auth = gp, ap
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p allower
			switch classify(r) {
			case ClassGeneral:
				p = general
			case ClassAuth:
				p = auth
			default:
				next.ServeHTTP(w, r)
				return
			}

			ok, retryAfter := p.allow(ClientIP(r))
			if !ok {
				reject(w, r, retryAfter)
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
