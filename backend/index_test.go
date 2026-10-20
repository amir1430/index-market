package main

import (
	"math"
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
	w := weighting{maxAge: 5 * time.Second, minSources: 2, halfLife: time.Minute, maxDev: 0.01, maxWeight: 1}
	b := newBook(w.halfLife)
	tick := func(ex, sym string, p, size float64, age time.Duration) {
		b.put(exchange.Tick{Exchange: ex, Symbol: sym, Price: p, Size: size, Time: now.Add(-time.Hour), Recv: now.Add(-age)})
	}
	// BTC: binance traded 3× bybit's quote volume (two trades); okx is stale; kucoin is off the median.
	tick("binance", "BTC/USDT", 100, 1, time.Second)
	tick("binance", "BTC/USDT", 100, 2, 0)
	tick("bybit", "BTC/USDT", 100.4, 1, time.Second)
	tick("okx", "BTC/USDT", 500, 9, 6*time.Second) // stale: left out despite the old trade time on the others
	tick("kucoin", "BTC/USDT", 102, 100, 0)        // 1.6% off the median (100.4): left out, however big
	tick("binance", "ETH/USDT", 10, 1, 0)
	b.put(exchange.Tick{Exchange: "binance", Symbol: "ETH/USDT", Price: 9, Size: 1, Time: now.Add(-2 * time.Hour), Recv: now}) // older trade, later in its batch

	idx := b.index([]string{"BTC/USDT", "ETH/USDT", "SOL/USDT"}, now, w)
	btc, eth, sol := idx[0], idx[1], idx[2]
	// binance's 1 s-old 100 decays to 100·2^(-1/60) before its 200 lands; bybit's 100.4 by the same.
	keep := math.Exp2(-1.0 / 60)
	vb, vy := 100*keep+200, 100.4*keep
	want := (100*vb + 100.4*vy) / (vb + vy)
	if math.Abs(btc.Price-want) > 1e-9 || btc.Stale || !slices.Equal(btc.Sources, []string{"binance", "bybit"}) {
		t.Errorf("BTC = %+v, want price %v", btc, want)
	}
	if math.Abs(btc.Weights[0]+btc.Weights[1]-1) > 1e-12 || btc.Weights[0] <= btc.Weights[1] {
		t.Errorf("BTC weights = %v", btc.Weights)
	}
	if eth.Price != 10 || !eth.Stale { // one source < minSources
		t.Errorf("ETH = %+v", eth)
	}
	if sol.Price != 0 || !sol.Stale || sol.Sources == nil || sol.Weights == nil { // [] in JSON, never null
		t.Errorf("SOL = %+v", sol)
	}

	// A cap of 50%: binance can't be the whole index.
	w.maxWeight = 0.5
	if btc := b.index([]string{"BTC/USDT"}, now, w)[0]; btc.Price != 100.2 {
		t.Errorf("capped BTC = %+v, want 100.2", btc)
	}

	btc.Price, btc.Sources = 101, []string{"binance", "bybit"}
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
}

func TestCapWeights(t *testing.T) {
	for _, c := range []struct {
		in   []float64
		max  float64
		want []float64
	}{
		{[]float64{6, 3, 1}, 1, []float64{0.6, 0.3, 0.1}},
		{[]float64{8, 1, 1}, 0.5, []float64{0.5, 0.25, 0.25}},
		{[]float64{90, 9, 1}, 0.4, []float64{0.4, 0.4, 0.2}},  // the excess cascades
		{[]float64{5, 0, 0}, 0.5, []float64{0.5, 0.25, 0.25}}, // no volume: equal shares of what's left
		{[]float64{0, 0}, 1, []float64{0.5, 0.5}},             // no volume yet
		{[]float64{9, 1}, 0.3, []float64{0.5, 0.5}},           // cap below 1/n: equal
		{[]float64{7}, 0.5, []float64{1}},
	} {
		got := capWeights(c.in, c.max)
		for i := range got {
			if math.Abs(got[i]-c.want[i]) > 1e-12 {
				t.Errorf("capWeights(%v, %v) = %v, want %v", c.in, c.max, got, c.want)
				break
			}
		}
	}
}

func TestConfig(t *testing.T) {
	t.Setenv("MARKETS", " btc/usdt,ETH/USDT,BTC/USDT ")
	c, err := loadConfig()
	if err != nil || !slices.Equal(c.markets, []string{"BTC/USDT", "ETH/USDT"}) || c.w.maxAge != 5*time.Second || c.w.minSources != 2 || c.w.halfLife != 15*time.Minute || c.w.maxDev != 0.005 || c.w.maxWeight != 0.5 {
		t.Errorf("loadConfig = %+v, %v", c, err)
	}
	for k, v := range map[string]string{"MARKETS": "BTC' OR 1=1", "INDEX_MIN_SOURCES": "8", "INDEX_EVERY": "0s", "INDEX_MAX_DEVIATION": "0", "INDEX_MAX_WEIGHT": "1.5", "INDEX_VOLUME_HALFLIFE": "-1m"} {
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
