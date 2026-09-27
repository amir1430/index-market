package exchange

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Binance: combined trade streams, symbols encoded in the URL.
var Binance = Exchange{
	Name:   "binance",
	Native: concat,
	URL: func(_ context.Context, natives []string) (string, error) {
		streams := make([]string, len(natives))
		for i, n := range natives {
			streams[i] = strings.ToLower(n) + "@trade"
		}
		return "wss://stream.binance.com:9443/stream?streams=" + strings.Join(streams, "/"), nil
	},
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		var m struct {
			Data struct {
				S string `json:"s"`
				P string `json:"p"`
				T int64  `json:"T"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		sym, ok := syms[m.Data.S]
		if !ok {
			return nil, nil, nil
		}
		p, err := strconv.ParseFloat(m.Data.P, 64)
		if err != nil {
			return nil, nil, err
		}
		return []Tick{{Symbol: sym, Price: p, Time: time.UnixMilli(m.Data.T)}}, nil, nil
	},
}
