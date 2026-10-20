package exchange

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Gate.io v4 spot trades. Keepalive is an application-level spot.ping.
var Gate = Exchange{
	Name:   "gate",
	Native: func(base, quote string) string { return base + "_" + quote },
	URL:    staticURL("wss://api.gateio.ws/ws/v4/"),
	Subscribe: func(natives []string) [][]byte {
		// One request per market, so an unknown one can't take the others down with it.
		msgs := make([][]byte, len(natives))
		for i, n := range natives {
			msgs[i] = fmt.Appendf(nil, `{"time":%d,"channel":"spot.trades","event":"subscribe","payload":["%s"]}`, time.Now().Unix(), n)
		}
		return msgs
	},
	Ping:      func() []byte { return fmt.Appendf(nil, `{"time":%d,"channel":"spot.ping"}`, time.Now().Unix()) },
	PingEvery: 20 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		var m struct {
			Channel string `json:"channel"`
			Event   string `json:"event"`
			Error   *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		if m.Error != nil {
			return nil, nil, fmt.Errorf("refused: %s", m.Error.Message) // e.g. a market Gate doesn't list
		}
		if m.Channel != "spot.trades" || m.Event != "update" {
			return nil, nil, nil // subscribe acks, pongs
		}
		var d struct {
			Pair   string `json:"currency_pair"`
			Price  string `json:"price"`
			Amount string `json:"amount"`
			TimeMs string `json:"create_time_ms"` // "1790000000000.123"
		}
		if err := json.Unmarshal(m.Result, &d); err != nil {
			return nil, nil, err
		}
		ms, _, _ := strings.Cut(d.TimeMs, ".")
		t, err := tradeTick(syms, d.Pair, d.Price, d.Amount, ms)
		return t, nil, err
	},
}
