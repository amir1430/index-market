package exchange

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Nobitex: Centrifugo JSON protocol. Quotes in IRT (rial) are normalized
// to TMN (toman = rial/10) so they line up with Wallex.
var Nobitex = Exchange{
	Name: "nobitex",
	Native: func(base, quote string) string {
		if quote == "TMN" {
			quote = "IRT"
		}
		return base + quote
	},
	URL: staticURL("wss://ws.nobitex.ir/connection/websocket"),
	Subscribe: func(natives []string) [][]byte {
		msgs := [][]byte{[]byte(`{"connect":{"name":"js"},"id":1}`)}
		for i, n := range natives {
			msgs = append(msgs, fmt.Appendf(nil, `{"subscribe":{"channel":"public:trades-%s"},"id":%d}`, n, i+2))
		}
		return msgs
	},
	// Centrifugo batches several JSON replies per frame, newline-separated.
	Parse: func(msg []byte, syms map[string]string) (ticks []Tick, replies [][]byte, err error) {
		for line := range bytes.Lines(msg) {
			t, r, err := nobitexLine(bytes.TrimSpace(line), syms)
			if err != nil {
				return nil, nil, err
			}
			ticks, replies = append(ticks, t...), append(replies, r...)
		}
		return ticks, replies, nil
	},
}

func nobitexLine(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
	if bytes.Equal(msg, []byte("{}")) {
		return nil, [][]byte{[]byte("{}")}, nil // server ping -> pong
	}
	var m struct {
		Push struct {
			Channel string `json:"channel"`
			Pub     struct {
				Data struct {
					Price string `json:"price"`
					Time  int64  `json:"time"`
				} `json:"data"`
			} `json:"pub"`
		} `json:"push"`
	}
	if err := json.Unmarshal(msg, &m); err != nil {
		return nil, nil, err
	}
	native, ok := strings.CutPrefix(m.Push.Channel, "public:trades-")
	sym, known := syms[native]
	if !ok || !known {
		return nil, nil, nil // connect/subscribe replies
	}
	p, err := strconv.ParseFloat(m.Push.Pub.Data.Price, 64)
	if err != nil {
		return nil, nil, err
	}
	if strings.HasSuffix(native, "IRT") {
		p /= 10
	}
	return []Tick{{Symbol: sym, Price: p, Time: time.UnixMilli(m.Push.Pub.Data.Time)}}, nil, nil
}
