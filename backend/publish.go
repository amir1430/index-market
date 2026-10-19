package main

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// publisher XADDs every index tick to the Valkey stream "index:<symbol>"
// (e.g. index:BTC/USDT). Entry fields, all strings:
//
//	price   decimal text, absent when no exchange is fresh
//	ts      unix ms of the tick
//	sources fresh exchanges, comma-separated ("binance,okx,bybit")
//	stale   "1" = fewer than INDEX_MIN_SOURCES fresh: never settle on it
//
// Consumers read with XREAD from their last ID (not consumer groups: every
// instance needs every tick) and replay a window with XRANGE.
type publisher struct {
	rdb     *redis.Client
	maxLen  int64
	failing bool // log state changes only, not every tick
}

// newPublisher parses VALKEY_URL, redis://user:password@host:6379/0
// (rediss:// for TLS). Empty url = publishing off.
func newPublisher(url string, maxLen int64) (*publisher, error) {
	if url == "" {
		log.Printf("VALKEY_URL not set: index ticks are not published")
		return nil, nil
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		// not err: a URL parse error quotes the URL, password included
		return nil, errors.New("bad VALKEY_URL, want redis://user:password@host:6379/0")
	}
	redis.SetLogger(quiet{}) // it logs every failed dial; publish logs state changes
	return &publisher{rdb: redis.NewClient(opt), maxLen: maxLen}, nil
}

type quiet struct{}

func (quiet) Printf(context.Context, string, ...any) {}

func streamKey(sym string) string { return "index:" + sym }

func fields(ip indexPrice) map[string]any {
	f := map[string]any{
		"ts":      strconv.FormatInt(ip.TS.UnixMilli(), 10),
		"sources": strings.Join(ip.Sources, ","),
		"stale":   "0",
	}
	if ip.Stale {
		f["stale"] = "1"
	}
	if len(ip.Sources) > 0 {
		f["price"] = strconv.FormatFloat(ip.Price, 'f', -1, 64)
	}
	return f
}

// publish sends one tick per market in one pipeline. It gives up after
// timeout so a slow Valkey never stalls the index loop.
func (p *publisher) publish(ctx context.Context, idx []indexPrice, timeout time.Duration) {
	if p == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	pipe := p.rdb.Pipeline()
	for _, ip := range idx {
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: streamKey(ip.Symbol), MaxLen: p.maxLen, Approx: true, Values: fields(ip)})
	}
	_, err := pipe.Exec(ctx)
	switch {
	case err != nil && !p.failing:
		log.Printf("valkey publish: %v", err)
		p.failing = true
	case err == nil && p.failing:
		log.Printf("valkey publish: recovered")
		p.failing = false
	}
}
