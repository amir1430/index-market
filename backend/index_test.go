package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"index-market/exchange"
)

func TestIndex(t *testing.T) {
	if m := median([]float64{3, 1, 2}); m != 2 {
		t.Errorf("odd median = %v", m)
	}
	if m := median([]float64{4, 1, 3, 2}); m != 2.5 {
		t.Errorf("even median = %v", m)
	}

	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	b := newBook()
	tick := func(ex, sym string, p float64, age time.Duration) {
		b.put(exchange.Tick{Exchange: ex, Symbol: sym, Price: p, Time: now.Add(-time.Hour), Recv: now.Add(-age)})
	}
	tick("bybit", "BTC/USDT", 102, time.Second)
	tick("binance", "BTC/USDT", 100, 0)
	tick("okx", "BTC/USDT", 500, 6*time.Second) // stale: left out despite the old trade time on the others
	tick("binance", "ETH/USDT", 10, 0)

	idx := b.index([]string{"BTC/USDT", "ETH/USDT", "SOL/USDT"}, now, 5*time.Second, 2)
	btc, eth, sol := idx[0], idx[1], idx[2]
	if btc.Price != 101 || btc.Stale || !slices.Equal(btc.Sources, []string{"binance", "bybit"}) {
		t.Errorf("BTC = %+v", btc)
	}
	if eth.Price != 10 || !eth.Stale { // one source < minSources
		t.Errorf("ETH = %+v", eth)
	}
	if sol.Price != 0 || !sol.Stale || sol.Sources == nil { // [] in JSON, never null
		t.Errorf("SOL = %+v", sol)
	}

	if got := indexLine(btc); got != `index_prices,symbol=BTC/USDT price=101,sources="binance,bybit",stale=false 1792411200000000000`+"\n" {
		t.Errorf("indexLine = %q", got)
	}
	if got := indexLine(sol); got != `index_prices,symbol=SOL/USDT sources="",stale=true 1792411200000000000`+"\n" {
		t.Errorf("indexLine stale = %q", got)
	}
	if f := fields(btc); f["price"] != "101" || f["stale"] != "0" || f["sources"] != "binance,bybit" || f["ts"] != "1792411200000" {
		t.Errorf("fields = %v", f)
	}
	if f := fields(sol); f["price"] != nil || f["stale"] != "1" {
		t.Errorf("fields stale = %v", f)
	}

	hist := withIndexSeries([]map[string]any{
		{"ts": "a", "exchange": "binance", "price": 10.0},
		{"ts": "a", "exchange": "okx", "price": 20.0},
		{"ts": "a", "exchange": "bybit", "price": nil}, // FILL(PREV) before first trade
		{"ts": "b", "exchange": "binance", "price": 30.0},
	})
	if got := hist[len(hist)-2:]; got[0]["price"] != 15.0 || got[1]["price"] != 30.0 || got[0]["exchange"] != "index" {
		t.Errorf("withIndexSeries tail = %v", got)
	}
}

func TestConfig(t *testing.T) {
	t.Setenv("MARKETS", " btc/usdt,ETH/USDT,BTC/USDT ")
	c, err := loadConfig()
	if err != nil || !slices.Equal(c.markets, []string{"BTC/USDT", "ETH/USDT"}) || c.maxAge != 5*time.Second || c.minSources != 2 {
		t.Errorf("loadConfig = %+v, %v", c, err)
	}
	for k, v := range map[string]string{"MARKETS": "BTC' OR 1=1", "INDEX_MIN_SOURCES": "4", "INDEX_EVERY": "0s"} {
		t.Run(k, func(t *testing.T) {
			t.Setenv(k, v)
			if _, err := loadConfig(); err == nil {
				t.Errorf("accepted %s=%s", k, v)
			}
		})
	}
	if _, err := newPublisher("redis://user:secret@host:6379/x", 1); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("newPublisher err = %v", err)
	}
}
