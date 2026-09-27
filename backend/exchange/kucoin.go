package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// KuCoin: needs a public token from REST before connecting, and app-level pings.
var KuCoin = Exchange{
	Name:   "kucoin",
	Native: func(base, quote string) string { return base + "-" + quote },
	URL: func(ctx context.Context, _ []string) (string, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.kucoin.com/api/v1/bullet-public", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var r struct {
			Code string `json:"code"`
			Data struct {
				Token     string `json:"token"`
				Instances []struct {
					Endpoint string `json:"endpoint"`
				} `json:"instanceServers"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return "", err
		}
		if r.Code != "200000" || len(r.Data.Instances) == 0 {
			return "", fmt.Errorf("bullet-public: code %s", r.Code)
		}
		return fmt.Sprintf("%s?token=%s&connectId=%d", r.Data.Instances[0].Endpoint, r.Data.Token, time.Now().UnixNano()), nil
	},
	Subscribe: func(natives []string) [][]byte {
		// ponytail: one topic for all symbols, KuCoin caps at 100 per subscribe
		return [][]byte{fmt.Appendf(nil, `{"id":"1","type":"subscribe","topic":"/market/ticker:%s","response":true}`, strings.Join(natives, ","))}
	},
	Ping:      func() []byte { return fmt.Appendf(nil, `{"id":"%d","type":"ping"}`, time.Now().UnixMilli()) },
	PingEvery: 18 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		var m struct {
			Type  string `json:"type"`
			Topic string `json:"topic"`
			Data  struct {
				Price string `json:"price"`
				Time  int64  `json:"time"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		if m.Type != "message" {
			return nil, nil, nil // welcome, ack, pong
		}
		_, native, _ := strings.Cut(m.Topic, ":")
		sym, ok := syms[native]
		if !ok {
			return nil, nil, nil
		}
		p, err := strconv.ParseFloat(m.Data.Price, 64)
		if err != nil {
			return nil, nil, err
		}
		return []Tick{{Symbol: sym, Price: p, Time: time.UnixMilli(m.Data.Time)}}, nil, nil
	},
}
