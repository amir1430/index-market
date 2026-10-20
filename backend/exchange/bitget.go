package exchange

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Bitget v2 spot trades. Keepalive is a plain-text "ping" -> "pong".
var Bitget = Exchange{
	Name:   "bitget",
	Native: concat,
	URL:    staticURL("wss://ws.bitget.com/v2/ws/public"),
	Subscribe: func(natives []string) [][]byte {
		// One request per market, so an unknown one can't take the others down with it.
		msgs := make([][]byte, len(natives))
		for i, n := range natives {
			msgs[i] = fmt.Appendf(nil, `{"op":"subscribe","args":[{"instType":"SPOT","channel":"trade","instId":"%s"}]}`, n)
		}
		return msgs
	},
	Ping:      func() []byte { return []byte("ping") },
	PingEvery: 25 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		if bytes.Equal(msg, []byte("pong")) {
			return nil, nil, nil
		}
		var m struct {
			Event  string `json:"event"`
			Msg    string `json:"msg"`
			Action string `json:"action"`
			Arg    struct {
				InstID string `json:"instId"`
			} `json:"arg"`
			Data []struct {
				Price string `json:"price"`
				Size  string `json:"size"`
				TS    string `json:"ts"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		if m.Event == "error" {
			return nil, nil, fmt.Errorf("refused: %s", m.Msg) // e.g. a market Bitget doesn't list
		}
		if m.Action == "snapshot" {
			// The last trades before we subscribed: counting them again on every
			// reconnect would inflate Bitget's volume.
			return nil, nil, nil
		}
		var ticks []Tick
		for _, d := range m.Data {
			t, err := tradeTick(syms, m.Arg.InstID, d.Price, d.Size, d.TS)
			if err != nil {
				return nil, nil, err
			}
			ticks = append(ticks, t...)
		}
		return ticks, nil, nil
	},
}
