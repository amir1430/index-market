package exchange

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// OKX v5 public trades. Keepalive is a plain-text "ping" -> "pong".
var OKX = Exchange{
	Name:   "okx",
	Native: func(base, quote string) string { return base + "-" + quote },
	URL:    staticURL("wss://ws.okx.com:8443/ws/v5/public"),
	Subscribe: func(natives []string) [][]byte {
		// One request per market, so an unknown one can't take the others down with it.
		msgs := make([][]byte, len(natives))
		for i, n := range natives {
			msgs[i] = fmt.Appendf(nil, `{"op":"subscribe","args":[{"channel":"trades","instId":"%s"}]}`, n)
		}
		return msgs
	},
	Ping:      func() []byte { return []byte("ping") },
	PingEvery: 20 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		if bytes.Equal(msg, []byte("pong")) {
			return nil, nil, nil
		}
		var m struct {
			Event string `json:"event"`
			Msg   string `json:"msg"`
			Data  []struct {
				InstID string `json:"instId"`
				Px     string `json:"px"`
				Sz     string `json:"sz"`
				TS     string `json:"ts"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		if m.Event == "error" {
			return nil, nil, fmt.Errorf("refused: %s", m.Msg) // e.g. a market OKX doesn't list
		}
		var ticks []Tick
		for _, d := range m.Data {
			t, err := tradeTick(syms, d.InstID, d.Px, d.Sz, d.TS)
			if err != nil {
				return nil, nil, err
			}
			ticks = append(ticks, t...)
		}
		return ticks, nil, nil // subscribe acks have no data
	},
}

// tradeTick builds a tick from string price, size and ms timestamp, nil if the symbol isn't ours.
func tradeTick(syms map[string]string, native, price, size, ms string) ([]Tick, error) {
	sym, ok := syms[native]
	if !ok {
		return nil, nil
	}
	p, err := strconv.ParseFloat(price, 64)
	if err != nil {
		return nil, err
	}
	sz, err := strconv.ParseFloat(size, 64)
	if err != nil {
		return nil, err
	}
	ts, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return nil, err
	}
	return []Tick{{Symbol: sym, Price: p, Size: sz, Time: time.UnixMilli(ts)}}, nil
}
