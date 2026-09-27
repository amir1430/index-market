package main

import (
	"slices"
	"time"
)

// Index price = plain median of each exchange's last price for a symbol.
// ponytail: unweighted median; switch to volume-weighted median once trade size is stored.

type indexPrice struct {
	Symbol  string   `json:"symbol"`
	Price   float64  `json:"price"`
	Sources []string `json:"sources"` // exchanges that contributed
	TS      string   `json:"ts"`      // newest contributing tick
}

func median(xs []float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// latestIndex computes one index per symbol from LATEST ON rows, skipping
// exchanges whose last tick is older than maxAge (disconnected or illiquid).
func latestIndex(rows []map[string]any, now time.Time, maxAge time.Duration) []indexPrice {
	type acc struct {
		prices  []float64
		sources []string
		newest  string
	}
	by := map[string]*acc{}
	var order []string
	for _, r := range rows {
		sym, _ := r["symbol"].(string)
		price, ok := r["price"].(float64)
		ts, _ := r["ts"].(string)
		t, err := time.Parse(time.RFC3339Nano, ts)
		if !ok || err != nil || now.Sub(t) > maxAge {
			continue
		}
		a := by[sym]
		if a == nil {
			a = &acc{}
			by[sym] = a
			order = append(order, sym)
		}
		a.prices = append(a.prices, price)
		a.sources = append(a.sources, r["exchange"].(string))
		a.newest = max(a.newest, ts) // same format, so lexical order = time order
	}
	out := make([]indexPrice, 0, len(order))
	for _, sym := range order {
		a := by[sym]
		out = append(out, indexPrice{Symbol: sym, Price: median(a.prices), Sources: a.sources, TS: a.newest})
	}
	return out
}

// withIndexSeries appends an exchange="index" row per timestamp holding the
// median of that bucket's prices. Rows come from SAMPLE BY ... FILL(PREV),
// so price is nil for exchanges before their first trade in range.
func withIndexSeries(rows []map[string]any) []map[string]any {
	by := map[string][]float64{}
	var order []string
	for _, r := range rows {
		p, ok := r["price"].(float64)
		if !ok {
			continue
		}
		ts := r["ts"].(string)
		if by[ts] == nil {
			order = append(order, ts)
		}
		by[ts] = append(by[ts], p)
	}
	for _, ts := range order {
		rows = append(rows, map[string]any{"ts": ts, "exchange": "index", "price": median(by[ts])})
	}
	return rows
}
