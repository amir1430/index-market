package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db := questDB{base: env("QUESTDB_URL", "http://localhost:9000")}
	if err := db.init(ctx); err != nil {
		log.Fatal(err)
	}

	ticks := make(chan exchange.Tick, 4096)
	st, err := openStreams(ctx, env("SQLITE_PATH", "pairs.db"), ticks)
	if err != nil {
		log.Fatalf("sqlite: %v", err)
	}
	st.startAll()
	go db.write(ctx, ticks)

	maxAge, err := time.ParseDuration(env("INDEX_MAX_AGE", "2m"))
	if err != nil {
		log.Fatalf("INDEX_MAX_AGE: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/latest", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.query(r.Context(), `SELECT exchange, symbol, price, ts FROM prices LATEST ON ts PARTITION BY exchange, symbol ORDER BY symbol, exchange`)
		if err != nil {
			queryFailed(w, err)
			return
		}
		// LATEST ON keeps returning pairs we no longer track; drop them.
		rows = slices.DeleteFunc(rows, func(r map[string]any) bool { s, _ := r["symbol"].(string); return !st.known(s) })
		writeJSON(w, map[string]any{"prices": rows, "index": latestIndex(rows, time.Now(), maxAge), "maxAgeMs": maxAge.Milliseconds()})
	})
	// range -> lookback + bucket size, keeps every chart at ~360 points
	ranges := map[string][2]string{"1h": {"dateadd('h', -1, now())", "10s"}, "6h": {"dateadd('h', -6, now())", "1m"}, "24h": {"dateadd('d', -1, now())", "4m"}}
	mux.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) {
		sym := r.URL.Query().Get("symbol")
		rg, ok := ranges[r.URL.Query().Get("range")]
		// Whitelist: symbol goes into SQL (QuestDB /exec has no bind params).
		if !st.known(sym) || !ok {
			http.Error(w, "unknown symbol or range", http.StatusBadRequest)
			return
		}
		// FILL(PREV): an exchange with no trade in a bucket keeps its last price,
		// so the per-bucket median doesn't jump when a quiet market skips one.
		// ponytail: carries a dead exchange's price forward within the range; cap by age if that matters
		rows, err := db.query(r.Context(), `SELECT ts, exchange, last(price) price FROM prices
			WHERE symbol = '`+sym+`' AND ts > `+rg[0]+`
			SAMPLE BY `+rg[1]+` FILL(PREV) ALIGN TO CALENDAR ORDER BY ts`)
		if err != nil {
			queryFailed(w, err)
			return
		}
		writeJSON(w, withIndexSeries(rows))
	})

	mux.HandleFunc("GET /api/pairs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, st.snapshot())
	})
	// Body: {"exchange":"binance","symbol":"SOL/USDT"}. POST adds, DELETE removes.
	// ponytail: no auth, anyone reaching the panel can edit pairs; add a token if it's exposed publicly
	setPair := func(on bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var b struct{ Exchange, Symbol string }
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&b); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			if err := st.set(b.Exchange, b.Symbol, on); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, st.snapshot())
		}
	}
	mux.HandleFunc("POST /api/pairs", setPair(true))
	mux.HandleFunc("DELETE /api/pairs", setPair(false))

	srv := &http.Server{Addr: env("ADDR", ":8080"), Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	log.Printf("listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func queryFailed(w http.ResponseWriter, err error) {
	log.Printf("query: %v", err)
	http.Error(w, "query failed", http.StatusBadGateway)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
