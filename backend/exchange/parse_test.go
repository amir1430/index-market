package exchange

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		ex      Exchange
		pair    string
		msg     string
		price   float64
		replies []string
	}{
		{Binance, "BTC/USDT", `{"stream":"btcusdt@trade","data":{"e":"trade","s":"BTCUSDT","p":"84732.21","T":1790000000000}}`, 84732.21, nil},
		{OKX, "BTC/USDT", `{"arg":{"channel":"trades","instId":"BTC-USDT"},"data":[{"instId":"BTC-USDT","tradeId":"1","px":"84730.1","sz":"0.1","side":"buy","ts":"1790000000000"}]}`, 84730.1, nil},
		{OKX, "BTC/USDT", `pong`, 0, nil},
		{Bybit, "BTC/USDT", `{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1790000000001,"data":[{"T":1790000000000,"s":"BTCUSDT","S":"Buy","v":"0.001","p":"84729.9","L":"PlusTick","i":"1","BT":false}]}`, 84729.9, nil},
		{Bybit, "BTC/USDT", `{"success":true,"ret_msg":"pong","conn_id":"x","op":"ping"}`, 0, nil},
	}
	for _, c := range cases {
		base, quote, _ := strings.Cut(c.pair, "/")
		syms := map[string]string{c.ex.Native(base, quote): c.pair}
		ticks, replies, err := c.ex.Parse([]byte(c.msg), syms)
		if err != nil {
			t.Fatalf("%s: %v", c.ex.Name, err)
		}
		if len(replies) != len(c.replies) || (len(replies) > 0 && string(replies[0]) != c.replies[0]) {
			t.Errorf("%s: replies %q, want %q", c.ex.Name, replies, c.replies)
		}
		if c.price == 0 {
			continue
		}
		if len(ticks) != 1 || ticks[0].Symbol != c.pair || ticks[0].Price != c.price || ticks[0].Time.Before(time.Unix(1e9, 0)) {
			t.Errorf("%s: got %+v, want %s @ %v", c.ex.Name, ticks, c.pair, c.price)
		}
	}
}

func TestRefusedSubscribe(t *testing.T) {
	for _, c := range []struct {
		ex  Exchange
		msg string
	}{
		{Bybit, `{"success":false,"ret_msg":"Invalid symbol :[publicTrade.TONUSDT]","conn_id":"x","op":"subscribe"}`},
		{OKX, `{"event":"error","code":"60018","msg":"Wrong URL or channel:trades,instId:NOPE-USDT doesn't exist.","connId":"x"}`},
	} {
		if _, _, err := c.ex.Parse([]byte(c.msg), nil); err == nil {
			t.Errorf("%s: refused subscribe not reported", c.ex.Name)
		}
		if n := len(c.ex.Subscribe([]string{"A", "B", "C"})); n != 3 {
			t.Errorf("%s: %d subscribe requests for 3 markets, want one each", c.ex.Name, n)
		}
	}
}
