package main

import (
	"context"
	"path/filepath"
	"testing"

	"index-market/exchange"
)

func TestStreamsPersist(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // stops streams restarted by set
	path := filepath.Join(t.TempDir(), "p.db")
	ticks := make(chan exchange.Tick, 1024)

	s, err := openStreams(ctx, path, ticks)
	if err != nil {
		t.Fatal(err)
	}
	if !s.known("USDT/TMN") || len(s.snapshot()["binance"]) != 2 {
		t.Fatalf("not seeded: %v", s.snapshot())
	}
	if err := s.set("binance", "sol/usdt", true); err != nil {
		t.Fatal(err)
	}
	if err := s.set("wallex", "USDT/TMN", false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][2]string{{"binance", "BTC' OR 1=1"}, {"nope", "BTC/USDT"}} {
		if s.set(bad[0], bad[1], true) == nil {
			t.Errorf("accepted %v", bad)
		}
	}

	s2, err := openStreams(ctx, path, ticks)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.known("SOL/USDT") || len(s2.snapshot()["wallex"]) != 2 {
		t.Fatalf("not persisted: %v", s2.snapshot())
	}
}
