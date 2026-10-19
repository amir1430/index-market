package main

import (
	"slices"
	"sync"
	"time"

	"index-market/exchange"
)

// Index price = median of the last trade price of every exchange whose last
// tick arrived within maxAge. Fewer than minSources fresh exchanges = stale:
// consumers must not settle on a stale tick.
// ponytail: unweighted median of last trades; mid-price (bookTicker) if it looks jumpy.

type indexPrice struct {
	Symbol  string    `json:"symbol"`
	Price   float64   `json:"price"` // 0 when no exchange is fresh
	Stale   bool      `json:"stale"`
	Sources []string  `json:"sources"` // fresh exchanges that make up the median
	TS      time.Time `json:"ts"`
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

// book holds the last tick of every exchange × market.
type book struct {
	mu   sync.Mutex
	last map[string]map[string]exchange.Tick // symbol -> exchange -> tick
}

func newBook() *book { return &book{last: map[string]map[string]exchange.Tick{}} }

func (b *book) put(t exchange.Tick) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.last[t.Symbol]
	if m == nil {
		m = map[string]exchange.Tick{}
		b.last[t.Symbol] = m
	}
	m[t.Exchange] = t
}

// ticks returns sym's last tick per exchange, in exchange.All() order.
func (b *book) ticks(sym string) []exchange.Tick {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []exchange.Tick
	for _, ex := range exchange.All() {
		if t, ok := b.last[sym][ex.Name]; ok {
			out = append(out, t)
		}
	}
	return out
}

func fresh(t exchange.Tick, now time.Time, maxAge time.Duration) bool {
	return now.Sub(t.Recv) <= maxAge
}

// index computes every market's index at now.
func (b *book) index(markets []string, now time.Time, maxAge time.Duration, minSources int) []indexPrice {
	out := make([]indexPrice, 0, len(markets))
	for _, sym := range markets {
		ip := indexPrice{Symbol: sym, TS: now, Sources: []string{}}
		var ps []float64
		for _, t := range b.ticks(sym) {
			if fresh(t, now, maxAge) {
				ps = append(ps, t.Price)
				ip.Sources = append(ip.Sources, t.Exchange)
			}
		}
		if len(ps) > 0 {
			ip.Price = median(ps)
		}
		ip.Stale = len(ps) < minSources
		out = append(out, ip)
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
