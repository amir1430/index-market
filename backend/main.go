package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"index-market/exchange"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type config struct {
	markets []string
	every   time.Duration // index tick interval
	w       weighting
	maxLen  int64 // Valkey stream length per market (approximate)
}

// Symbols end up inside QuestDB SQL (no bind params there), so keep them strict.
var pairRe = regexp.MustCompile(`^[A-Z0-9]{1,15}/[A-Z0-9]{1,15}$`)

func loadConfig() (config, error) {
	var c config
	for _, m := range strings.Split(env("MARKETS", "BTC/USDT,ETH/USDT"), ",") {
		m = strings.ToUpper(strings.TrimSpace(m))
		if !pairRe.MatchString(m) {
			return c, fmt.Errorf("MARKETS: bad market %q, want BASE/QUOTE", m)
		}
		if !slices.Contains(c.markets, m) {
			c.markets = append(c.markets, m)
		}
	}
	var err error
	if c.every, err = time.ParseDuration(env("INDEX_EVERY", "250ms")); err != nil || c.every <= 0 {
		return c, fmt.Errorf("INDEX_EVERY: want a duration like 250ms")
	}
	if c.w.maxAge, err = time.ParseDuration(env("INDEX_MAX_AGE", "5s")); err != nil || c.w.maxAge <= 0 {
		return c, fmt.Errorf("INDEX_MAX_AGE: want a duration like 5s")
	}
	if c.w.minSources, err = strconv.Atoi(env("INDEX_MIN_SOURCES", "2")); err != nil || c.w.minSources < 1 || c.w.minSources > len(exchange.All()) {
		return c, fmt.Errorf("INDEX_MIN_SOURCES: want 1..%d", len(exchange.All()))
	}
	if c.w.halfLife, err = time.ParseDuration(env("INDEX_VOLUME_HALFLIFE", "15m")); err != nil || c.w.halfLife <= 0 {
		return c, fmt.Errorf("INDEX_VOLUME_HALFLIFE: want a duration like 15m")
	}
	if c.w.maxDev, err = strconv.ParseFloat(env("INDEX_MAX_DEVIATION", "0.005"), 64); err != nil || !(c.w.maxDev > 0 && c.w.maxDev < 1) {
		return c, fmt.Errorf("INDEX_MAX_DEVIATION: want a fraction like 0.005 (0.5%%)")
	}
	if c.w.maxWeight, err = strconv.ParseFloat(env("INDEX_MAX_WEIGHT", "0.5"), 64); err != nil || !(c.w.maxWeight > 0 && c.w.maxWeight <= 1) {
		return c, fmt.Errorf("INDEX_MAX_WEIGHT: want a fraction like 0.5, at most 1")
	}
	if c.maxLen, err = strconv.ParseInt(env("VALKEY_MAXLEN", "14400"), 10, 64); err != nil || c.maxLen < 1 {
		return c, fmt.Errorf("VALKEY_MAXLEN: want a positive number")
	}
	return c, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	pub, err := newPublisher(os.Getenv("VALKEY_URL"), cfg.maxLen)
	if err != nil {
		log.Fatal(err)
	}
	db := questDB{base: env("QUESTDB_URL", "http://localhost:9000")}
	if err := db.init(ctx); err != nil {
		log.Fatal(err)
	}
	log.Printf("markets %v from %d exchanges, index every %v", cfg.markets, len(exchange.All()), cfg.every)

	lines := make(chan string, 16384)
	written := make(chan struct{})
	go func() {
		db.write(ctx, lines)
		close(written)
	}()

	bk := newBook(cfg.w.halfLife)
	ticks := make(chan exchange.Tick, 4096)
	for _, ex := range exchange.All() {
		go exchange.Run(ctx, ex, cfg.markets, ticks)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticks:
				bk.put(t)
				send(lines, tickLine(t))
			}
		}
	}()

	var latest atomic.Pointer[[]indexPrice]
	first := bk.index(cfg.markets, time.Now().UTC(), cfg.w)
	latest.Store(&first)
	go func() {
		t := time.NewTicker(cfg.every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				idx := bk.index(cfg.markets, now.UTC(), cfg.w)
				latest.Store(&idx)
				for _, ip := range idx {
					send(lines, indexLine(ip))
				}
				pub.publish(ctx, idx, cfg.every)
			}
		}
	}()

	mux := http.NewServeMux()
	// The public endpoint: every market with its index and each exchange's last price.
	mux.HandleFunc("GET /api/markets", func(w http.ResponseWriter, r *http.Request) {
		type source struct {
			Exchange string    `json:"exchange"`
			Price    float64   `json:"price"`
			TS       time.Time `json:"ts"`     // trade time
			Fresh    bool      `json:"fresh"`  // heard from within maxAge
			Weight   float64   `json:"weight"` // share of the index, 0 = not counted (stale or off the median)
		}
		type market struct {
			Symbol    string     `json:"symbol"`
			Base      string     `json:"base"`
			Quote     string     `json:"quote"`
			Stream    string     `json:"stream"` // Valkey stream key
			Index     indexPrice `json:"index"`
			Exchanges []source   `json:"exchanges"`
		}
		idx := *latest.Load()
		out := make([]market, len(idx))
		for i, ip := range idx {
			base, quote, _ := strings.Cut(ip.Symbol, "/")
			m := market{Symbol: ip.Symbol, Base: base, Quote: quote, Stream: streamKey(ip.Symbol), Index: ip, Exchanges: []source{}}
			for _, t := range bk.ticks(ip.Symbol) {
				var wt float64
				if i := slices.Index(ip.Sources, t.Exchange); i >= 0 {
					wt = ip.Weights[i]
				}
				m.Exchanges = append(m.Exchanges, source{t.Exchange, t.Price, t.Time.UTC(), fresh(t, ip.TS, cfg.w.maxAge), wt})
			}
			out[i] = m
		}
		writeJSON(w, map[string]any{
			"markets":    out,
			"maxAgeMs":   cfg.w.maxAge.Milliseconds(),
			"minSources": cfg.w.minSources,
			"maxDev":     cfg.w.maxDev,
			"maxWeight":  cfg.w.maxWeight,
			"everyMs":    cfg.every.Milliseconds(),
		})
	})
	// Panel chart. range -> lookback + bucket size, keeps every chart at ~360 points
	ranges := map[string][2]string{"1h": {"dateadd('h', -1, now())", "10s"}, "6h": {"dateadd('h', -6, now())", "1m"}, "24h": {"dateadd('d', -1, now())", "4m"}}
	mux.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) {
		sym := r.URL.Query().Get("symbol")
		rg, ok := ranges[r.URL.Query().Get("range")]
		// Whitelist: symbol goes into SQL (QuestDB /exec has no bind params).
		if !slices.Contains(cfg.markets, sym) || !ok {
			http.Error(w, "unknown symbol or range", http.StatusBadRequest)
			return
		}
		// FILL(PREV): an exchange with no trade in a bucket keeps its last price,
		// so a quiet market doesn't leave holes in its line.
		// ponytail: carries a dead exchange's price forward within the range; cap by age if that matters
		rows, err := db.query(r.Context(), `SELECT ts, exchange, last(price) price FROM prices
			WHERE symbol = '`+sym+`' AND ts > `+rg[0]+`
			SAMPLE BY `+rg[1]+` FILL(PREV) ALIGN TO CALENDAR ORDER BY ts`)
		if err == nil {
			// The index line is the index as published (volume-weighted), not recomputed.
			var idx []map[string]any
			idx, err = db.query(r.Context(), `SELECT ts, last(price) price FROM index_prices
				WHERE symbol = '`+sym+`' AND ts > `+rg[0]+`
				SAMPLE BY `+rg[1]+` FILL(PREV) ALIGN TO CALENDAR ORDER BY ts`)
			for _, row := range idx {
				row["exchange"] = "index"
			}
			rows = append(rows, idx...)
		}
		if err != nil {
			log.Printf("query: %v", err)
			http.Error(w, "query failed", http.StatusBadGateway)
			return
		}
		writeJSON(w, rows)
	})

	srv := &http.Server{Addr: env("ADDR", ":8080"), Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	log.Printf("listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-written
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
