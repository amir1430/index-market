package exchange

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Bybit v5 spot public trades. Keepalive is {"op":"ping"}, answered in JSON.
var Bybit = Exchange{
	Name:   "bybit",
	Native: concat,
	URL:    staticURL("wss://stream.bybit.com/v5/public/spot"),
	Subscribe: func(natives []string) [][]byte {
		// One request per market: Bybit refuses a whole request over one unknown
		// symbol (and over 10 args), losing every other market in it.
		msgs := make([][]byte, len(natives))
		for i, n := range natives {
			msgs[i] = []byte(`{"op":"subscribe","args":["publicTrade.` + n + `"]}`)
		}
		return msgs
	},
	Ping:      func() []byte { return []byte(`{"op":"ping"}`) },
	PingEvery: 20 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		var m struct {
			Success *bool  `json:"success"`
			RetMsg  string `json:"ret_msg"`
			Data    []struct {
				S    string `json:"s"`
				Side string `json:"S"` // declared so "S" doesn't case-insensitively overwrite "s"
				P    string `json:"p"`
				V    string `json:"v"`
				T    int64  `json:"T"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		if m.Success != nil && !*m.Success {
			return nil, nil, fmt.Errorf("refused: %s", m.RetMsg) // e.g. a market Bybit doesn't list
		}
		var ticks []Tick
		for _, d := range m.Data {
			t, err := tradeTick(syms, d.S, d.P, d.V, strconv.FormatInt(d.T, 10))
			if err != nil {
				return nil, nil, err
			}
			ticks = append(ticks, t...)
		}
		return ticks, nil, nil // subscribe acks / pongs have no data
	},
}
