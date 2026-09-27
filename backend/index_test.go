package main

import (
	"testing"
	"time"
)

func TestIndex(t *testing.T) {
	if m := median([]float64{3, 1, 2}); m != 2 {
		t.Errorf("odd median = %v", m)
	}
	if m := median([]float64{4, 1, 3, 2}); m != 2.5 {
		t.Errorf("even median = %v", m)
	}

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	rows := []map[string]any{
		{"exchange": "binance", "symbol": "BTC/USDT", "price": 100.0, "ts": "2026-09-27T11:59:59.000000Z"},
		{"exchange": "kucoin", "symbol": "BTC/USDT", "price": 102.0, "ts": "2026-09-27T11:59:58.000000Z"},
		{"exchange": "nobitex", "symbol": "BTC/USDT", "price": 101.0, "ts": "2026-09-27T11:59:50.000000Z"},
		{"exchange": "wallex", "symbol": "BTC/USDT", "price": 500.0, "ts": "2026-09-27T11:00:00.000000Z"}, // stale, excluded
	}
	idx := latestIndex(rows, now, time.Minute)
	if len(idx) != 1 || idx[0].Price != 101 || len(idx[0].Sources) != 3 || idx[0].TS != "2026-09-27T11:59:59.000000Z" {
		t.Errorf("latestIndex = %+v", idx)
	}

	hist := withIndexSeries([]map[string]any{
		{"ts": "a", "exchange": "binance", "price": 10.0},
		{"ts": "a", "exchange": "kucoin", "price": 20.0},
		{"ts": "a", "exchange": "nobitex", "price": nil}, // FILL(PREV) before first trade
		{"ts": "b", "exchange": "binance", "price": 30.0},
	})
	if got := hist[len(hist)-2:]; got[0]["price"] != 15.0 || got[1]["price"] != 30.0 || got[0]["exchange"] != "index" {
		t.Errorf("withIndexSeries tail = %v", got)
	}
}
