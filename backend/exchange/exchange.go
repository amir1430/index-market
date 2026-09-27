// Package exchange streams last-trade prices from exchanges over websocket
// and normalizes them into Ticks.
package exchange

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Tick is the normalized price update every exchange is parsed into.
// Symbol is "BASE/QUOTE" upper-case, e.g. "BTC/USDT", "USDT/TMN".
type Tick struct {
	Exchange string
	Symbol   string
	Price    float64
	Time     time.Time
}

// Exchange describes how to talk to one venue. Only Parse is required
// besides Name/Native/URL; everything protocol-specific lives in these funcs.
type Exchange struct {
	Name string
	// Native converts a normalized pair ("BTC", "USDT") to the exchange's symbol.
	Native func(base, quote string) string
	// URL returns the websocket endpoint (may do HTTP, e.g. KuCoin token).
	URL func(ctx context.Context, natives []string) (string, error)
	// Subscribe returns messages to send right after connecting.
	Subscribe func(natives []string) [][]byte
	// Parse turns one frame into ticks. syms maps native symbol -> normalized.
	// replies are written back (protocol pongs, handshakes).
	Parse func(msg []byte, syms map[string]string) (ticks []Tick, replies [][]byte, err error)
	// Ping, if set, is sent every PingEvery.
	Ping      func() []byte
	PingEvery time.Duration
}

const readTimeout = 60 * time.Second

// Run streams ticks for pairs ("BTC/USDT") into out until ctx is done,
// reconnecting on any error.
func Run(ctx context.Context, ex Exchange, pairs []string, out chan<- Tick) {
	syms := map[string]string{}
	natives := make([]string, 0, len(pairs))
	for _, p := range pairs {
		base, quote, ok := strings.Cut(p, "/")
		if !ok {
			log.Printf("%s: bad pair %q, want BASE/QUOTE", ex.Name, p)
			continue
		}
		n := ex.Native(base, quote)
		syms[n] = p
		natives = append(natives, n)
	}
	for ctx.Err() == nil {
		err := runOnce(ctx, ex, natives, syms, out)
		if ctx.Err() != nil {
			return
		}
		log.Printf("%s: %v; reconnecting in 3s", ex.Name, err)
		// ponytail: fixed backoff, add exponential if an exchange starts rate-limiting reconnects
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}

func runOnce(ctx context.Context, ex Exchange, natives []string, syms map[string]string, out chan<- Tick) error {
	url, err := ex.URL(ctx, natives)
	if err != nil {
		return fmt.Errorf("url: %w", err)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		conn.Close()
	}()

	var mu sync.Mutex // gorilla allows one concurrent writer
	write := func(b []byte) error {
		mu.Lock()
		defer mu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, b)
	}
	// Protocol-level pings (Binance) keep the connection alive too.
	conn.SetPingHandler(func(s string) error {
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		return conn.WriteControl(websocket.PongMessage, []byte(s), time.Now().Add(5*time.Second))
	})

	if ex.Subscribe != nil {
		for _, m := range ex.Subscribe(natives) {
			if err := write(m); err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}
		}
	}
	if ex.Ping != nil {
		go func() {
			t := time.NewTicker(ex.PingEvery)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					if write(ex.Ping()) != nil {
						return
					}
				}
			}
		}()
	}

	for {
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		ticks, replies, err := ex.Parse(msg, syms)
		if err != nil {
			log.Printf("%s: parse: %v: %.200s", ex.Name, err, msg)
			continue
		}
		for _, r := range replies {
			if err := write(r); err != nil {
				return fmt.Errorf("reply: %w", err)
			}
		}
		for _, t := range ticks {
			t.Exchange = ex.Name
			select {
			case out <- t:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// staticURL is for exchanges whose endpoint doesn't depend on symbols.
func staticURL(u string) func(context.Context, []string) (string, error) {
	return func(context.Context, []string) (string, error) { return u, nil }
}

func concat(base, quote string) string { return base + quote }

// All returns every supported exchange.
func All() []Exchange { return []Exchange{Binance, KuCoin, Wallex, Nobitex} }
