package main

import (
	"math"
	"slices"
	"sync"
	"time"

	"index-market/exchange"
)

// Index price = volume-weighted mean of the last trade price of every exchange
// whose last tick arrived within maxAge:
//   - an exchange's weight is its recent quote volume (price × size of its
//     trades, decayed with a half-life), so busier venues count for more;
//   - a price further than maxDev from the median of the fresh prices is left
//     out (a glitching venue can't drag the index, however big its volume);
//   - no exchange weighs more than maxWeight, the excess spread over the
//     others in proportion (one venue can't be the whole index).
//
// Fewer than minSources counted exchanges = stale: consumers must not settle
// on a stale tick.

type indexPrice struct {
	Symbol  string    `json:"symbol"`
	Price   float64   `json:"price"` // 0 when no exchange is fresh
	Stale   bool      `json:"stale"`
	Sources []string  `json:"sources"` // exchanges counted in the index
	Weights []float64 `json:"weights"` // each source's share, same order, sums to 1
	TS      time.Time `json:"ts"`
}

// weighting holds the index's knobs (from INDEX_* env).
type weighting struct {
	maxAge     time.Duration // an exchange with no tick for this long leaves the index
	minSources int           // fewer counted exchanges = stale
	halfLife   time.Duration // volume decay
	maxDev     float64       // max distance from the median, fraction (0.005 = 0.5%)
	maxWeight  float64       // max share of one exchange, fraction
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

// capWeights scales ws to sum 1 with no share above max, the excess going to
// the others in proportion (equally among those with no volume). With max at or
// below 1/n every share is 1/n.
func capWeights(ws []float64, max float64) []float64 {
	n := len(ws)
	out := make([]float64, n)
	capped := make([]bool, n)
	if max*float64(n) <= 1 {
		max = 1 / float64(n)
	}
	for left := 1.0; ; {
		var sum float64
		free := 0
		for i, w := range ws {
			if !capped[i] {
				sum += w
				free++
			}
		}
		over := false
		for i, w := range ws {
			if capped[i] {
				continue
			}
			if sum > 0 {
				out[i] = left * w / sum
			} else {
				out[i] = left / float64(free)
			}
			if out[i] > max+1e-12 {
				out[i], capped[i], over = max, true, true
				left -= max
			}
		}
		if !over {
			return out
		}
	}
}

// slot is one exchange × market: its last tick and its decayed quote volume
// as of that tick's receive time.
type slot struct {
	tick exchange.Tick
	vol  float64
}

// book holds the last tick and volume of every exchange × market.
type book struct {
	mu       sync.Mutex
	halfLife time.Duration
	last     map[string]map[string]slot // symbol -> exchange -> slot
}

func newBook(halfLife time.Duration) *book {
	return &book{halfLife: halfLife, last: map[string]map[string]slot{}}
}

// decay is the factor volume keeps after d.
func (b *book) decay(d time.Duration) float64 {
	if d <= 0 {
		return 1
	}
	return math.Exp2(-float64(d) / float64(b.halfLife))
}

func (b *book) put(t exchange.Tick) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.last[t.Symbol]
	if m == nil {
		m = map[string]slot{}
		b.last[t.Symbol] = m
	}
	s := m[t.Exchange]
	vol := s.vol*b.decay(t.Recv.Sub(s.tick.Recv)) + t.Price*t.Size
	if s.tick.Time.After(t.Time) {
		// An older trade (some exchanges send a batch newest first): its volume
		// counts, the price stays the newest trade's.
		t.Price, t.Time = s.tick.Price, s.tick.Time
	}
	m[t.Exchange] = slot{tick: t, vol: vol}
}

// slots returns sym's slot per exchange, in exchange.All() order, volumes decayed to now.
func (b *book) slots(sym string, now time.Time) []slot {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []slot
	for _, ex := range exchange.All() {
		if s, ok := b.last[sym][ex.Name]; ok {
			s.vol *= b.decay(now.Sub(s.tick.Recv))
			out = append(out, s)
		}
	}
	return out
}

// ticks returns sym's last tick per exchange, in exchange.All() order.
func (b *book) ticks(sym string) []exchange.Tick {
	var out []exchange.Tick
	for _, s := range b.slots(sym, time.Time{}) {
		out = append(out, s.tick)
	}
	return out
}

func fresh(t exchange.Tick, now time.Time, maxAge time.Duration) bool {
	return now.Sub(t.Recv) <= maxAge
}

// index computes every market's index at now.
func (b *book) index(markets []string, now time.Time, w weighting) []indexPrice {
	out := make([]indexPrice, 0, len(markets))
	for _, sym := range markets {
		ip := indexPrice{Symbol: sym, TS: now, Sources: []string{}, Weights: []float64{}}
		var live []slot
		var ps []float64
		for _, s := range b.slots(sym, now) {
			if fresh(s.tick, now, w.maxAge) {
				live = append(live, s)
				ps = append(ps, s.tick.Price)
			}
		}
		var kept []slot
		var vols []float64
		if len(ps) > 0 {
			med := median(ps)
			for _, s := range live {
				if math.Abs(s.tick.Price/med-1) <= w.maxDev {
					kept = append(kept, s)
					vols = append(vols, s.vol)
				}
			}
		}
		if len(kept) > 0 {
			ws := capWeights(vols, w.maxWeight)
			for i, s := range kept {
				ip.Price += ws[i] * s.tick.Price
				ip.Sources = append(ip.Sources, s.tick.Exchange)
				ip.Weights = append(ip.Weights, ws[i])
			}
		}
		ip.Stale = len(kept) < w.minSources
		out = append(out, ip)
	}
	return out
}
