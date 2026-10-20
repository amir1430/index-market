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

// KuCoin spot trades (/market/match, every fill with its size; /market/ticker is
// throttled and would undercount volume). Needs a public token from REST before
// connecting, and app-level pings.
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
		// One request per market, so an unknown one can't take the others down with it.
		msgs := make([][]byte, len(natives))
		for i, n := range natives {
			msgs[i] = fmt.Appendf(nil, `{"id":"%d","type":"subscribe","topic":"/market/match:%s","response":true}`, i+1, n)
		}
		return msgs
	},
	Ping:      func() []byte { return fmt.Appendf(nil, `{"id":"%d","type":"ping"}`, time.Now().UnixMilli()) },
	PingEvery: 18 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		var m struct {
			Type  string `json:"type"`
			Topic string `json:"topic"`
			Data  struct {
				Price string `json:"price"`
				Size  string `json:"size"`
				Time  string `json:"time"` // ns
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			return nil, nil, err
		}
		if m.Type == "error" {
			return nil, nil, fmt.Errorf("refused: %s", msg) // e.g. a market KuCoin doesn't list
		}
		if m.Type != "message" {
			return nil, nil, nil // welcome, ack, pong
		}
		_, native, _ := strings.Cut(m.Topic, ":")
		ns, err := strconv.ParseInt(m.Data.Time, 10, 64)
		if err != nil {
			return nil, nil, err
		}
		t, err := tradeTick(syms, native, m.Data.Price, m.Data.Size, strconv.FormatInt(ns/1e6, 10))
		return t, nil, err
	},
}
