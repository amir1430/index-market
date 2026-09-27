package exchange

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Bybit v5 spot public trades. Keepalive is {"op":"ping"}, answered in JSON.
var Bybit = Exchange{
	Name:   "bybit",
	Native: concat,
	URL:    staticURL("wss://stream.bybit.com/v5/public/spot"),
	Subscribe: func(natives []string) [][]byte {
		// ponytail: one request, Bybit spot caps args at 10 per subscribe
		args := make([]string, len(natives))
		for i, n := range natives {
			args[i] = `"publicTrade.` + n + `"`
		}
		return [][]byte{fmt.Appendf(nil, `{"op":"subscribe","args":[%s]}`, strings.Join(args, ","))}
	},
	Ping:      func() []byte { return []byte(`{"op":"ping"}`) },
	PingEvery: 20 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		var m struct {
			Data []struct {
				S    string `json:"s"`
				Side string `json:"S"` // declared so "S" doesn't case-insensitively overwrite "s"
				P    string `json:"p"`
				T    int64  `json:"T"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		var ticks []Tick
		for _, d := range m.Data {
			t, err := tradeTick(syms, d.S, d.P, strconv.FormatInt(d.T, 10))
			if err != nil {
				return nil, nil, err
			}
			ticks = append(ticks, t...)
		}
		return ticks, nil, nil // subscribe acks / pongs have no data
	},
}
