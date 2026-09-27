package exchange

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Wallex speaks socket.io v2 (Engine.IO v3) over raw websocket:
//
//	server "0{...}", "40"  -> client "42[subscribe]" per symbol
//	client "2" every 25s   -> server "3"   (v3: client pings)
//	server `42["Broadcaster","BTCUSDT@trade",{...}]` -> ticks
var Wallex = Exchange{
	Name:      "wallex",
	Native:    concat,
	URL:       staticURL("wss://api.wallex.ir/socket.io/?EIO=3&transport=websocket"),
	Ping:      func() []byte { return []byte("2") },
	PingEvery: 25 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		switch {
		case bytes.HasPrefix(msg, []byte("40")):
			var subs [][]byte
			for n := range syms {
				subs = append(subs, fmt.Appendf(nil, `42["subscribe",{"channel":"%s@trade"}]`, n))
			}
			return nil, subs, nil
		case !bytes.HasPrefix(msg, []byte("42")):
			return nil, nil, nil
		}
		var ev []json.RawMessage
		if err := json.Unmarshal(msg[2:], &ev); err != nil {
			return nil, nil, err
		}
		var name, channel string
		if len(ev) < 3 || json.Unmarshal(ev[0], &name) != nil || name != "Broadcaster" || json.Unmarshal(ev[1], &channel) != nil {
			return nil, nil, nil
		}
		native, kind, _ := strings.Cut(channel, "@")
		sym, ok := syms[native]
		if !ok || kind != "trade" {
			return nil, nil, nil
		}
		var d struct {
			Price     string `json:"price"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(ev[2], &d); err != nil {
			return nil, nil, err
		}
		p, err := strconv.ParseFloat(d.Price, 64)
		if err != nil {
			return nil, nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, d.Timestamp)
		if err != nil {
			t = time.Now()
		}
		return []Tick{{Symbol: sym, Price: p, Time: t}}, nil, nil
	},
}
