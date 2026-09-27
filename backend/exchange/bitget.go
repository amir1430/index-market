package exchange

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Bitget v2 spot trades. Keepalive is a plain-text "ping" -> "pong".
var Bitget = Exchange{
	Name:   "bitget",
	Native: concat,
	URL:    staticURL("wss://ws.bitget.com/v2/ws/public"),
	Subscribe: func(natives []string) [][]byte {
		args := make([]string, len(natives))
		for i, n := range natives {
			args[i] = fmt.Sprintf(`{"instType":"SPOT","channel":"trade","instId":"%s"}`, n)
		}
		return [][]byte{fmt.Appendf(nil, `{"op":"subscribe","args":[%s]}`, strings.Join(args, ","))}
	},
	Ping:      func() []byte { return []byte("ping") },
	PingEvery: 25 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		if bytes.Equal(msg, []byte("pong")) {
			return nil, nil, nil
		}
		var m struct {
			Arg struct {
				InstID string `json:"instId"`
			} `json:"arg"`
			Data []struct {
				Price string `json:"price"`
				TS    string `json:"ts"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		var ticks []Tick
		for _, d := range m.Data {
			t, err := tradeTick(syms, m.Arg.InstID, d.Price, d.TS)
			if err != nil {
				return nil, nil, err
			}
			ticks = append(ticks, t...)
		}
		return ticks, nil, nil
	},
}
