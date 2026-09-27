package exchange

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// OKX v5 public trades. Keepalive is a plain-text "ping" -> "pong".
var OKX = Exchange{
	Name:   "okx",
	Native: func(base, quote string) string { return base + "-" + quote },
	URL:    staticURL("wss://ws.okx.com:8443/ws/v5/public"),
	Subscribe: func(natives []string) [][]byte {
		args := make([]string, len(natives))
		for i, n := range natives {
			args[i] = fmt.Sprintf(`{"channel":"trades","instId":"%s"}`, n)
		}
		return [][]byte{fmt.Appendf(nil, `{"op":"subscribe","args":[%s]}`, strings.Join(args, ","))}
	},
	Ping:      func() []byte { return []byte("ping") },
	PingEvery: 20 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		if bytes.Equal(msg, []byte("pong")) {
			return nil, nil, nil
		}
		var m struct {
			Data []struct {
				InstID string `json:"instId"`
				Px     string `json:"px"`
				TS     string `json:"ts"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		var ticks []Tick
		for _, d := range m.Data {
			t, err := tradeTick(syms, d.InstID, d.Px, d.TS)
			if err != nil {
				return nil, nil, err
			}
			ticks = append(ticks, t...)
		}
		return ticks, nil, nil // subscribe acks have no data
	},
}

// tradeTick builds a tick from string price and ms timestamp, nil if the symbol isn't ours.
func tradeTick(syms map[string]string, native, price, ms string) ([]Tick, error) {
	sym, ok := syms[native]
	if !ok {
		return nil, nil
	}
	p, err := strconv.ParseFloat(price, 64)
	if err != nil {
		return nil, err
	}
	ts, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return nil, err
	}
	return []Tick{{Symbol: sym, Price: p, Time: time.UnixMilli(ts)}}, nil
}
