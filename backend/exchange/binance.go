package exchange

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// Binance: combined trade streams, symbols encoded in the URL.
// data-stream.binance.vision is Binance's market-data-only host on :443;
// stream.binance.com:9443 is often firewalled or geo-blocked on cloud hosts.
var Binance = Exchange{
	Name:   "binance",
	Native: concat,
	URL: func(_ context.Context, natives []string) (string, error) {
		streams := make([]string, len(natives))
		for i, n := range natives {
			streams[i] = strings.ToLower(n) + "@trade"
		}
		return "wss://data-stream.binance.vision/stream?streams=" + strings.Join(streams, "/"), nil
	},
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		var m struct {
			Data struct {
				S string `json:"s"`
				P string `json:"p"`
				Q string `json:"q"`
				T int64  `json:"T"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		t, err := tradeTick(syms, m.Data.S, m.Data.P, m.Data.Q, strconv.FormatInt(m.Data.T, 10))
		return t, nil, err
	},
}
