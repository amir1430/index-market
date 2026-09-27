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
		{KuCoin, "BTC/USDT", `{"type":"message","topic":"/market/ticker:BTC-USDT","subject":"trade.ticker","data":{"price":"84735.7","time":1790000000000}}`, 84735.7, nil},
		{Wallex, "USDT/TMN", `42["Broadcaster","USDTTMN@trade",{"isBuyOrder":false,"quantity":"705.0","price":"31704.0000000000000000","timestamp":"2022-06-26T11:14:09Z"}]`, 31704, nil},
		{Wallex, "USDT/TMN", `40`, 0, []string{`42["subscribe",{"channel":"USDTTMN@trade"}]`}},
		{OKX, "BTC/USDT", `{"arg":{"channel":"trades","instId":"BTC-USDT"},"data":[{"instId":"BTC-USDT","tradeId":"1","px":"84730.1","sz":"0.1","side":"buy","ts":"1790000000000"}]}`, 84730.1, nil},
		{OKX, "BTC/USDT", `pong`, 0, nil},
		{Bitget, "BTC/USDT", `{"action":"update","arg":{"instType":"SPOT","channel":"trade","instId":"BTCUSDT"},"data":[{"ts":"1790000000000","price":"84731.5","size":"0.01","side":"buy","tradeId":"1"}],"ts":1790000000001}`, 84731.5, nil},
		{Bybit, "BTC/USDT", `{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1790000000001,"data":[{"T":1790000000000,"s":"BTCUSDT","S":"Buy","v":"0.001","p":"84729.9","L":"PlusTick","i":"1","BT":false}]}`, 84729.9, nil},
		{Bybit, "BTC/USDT", `{"success":true,"ret_msg":"pong","conn_id":"x","op":"ping"}`, 0, nil},
		// batched frame: connect reply + ping + IRT publication (rial -> toman)
		{Nobitex, "BTC/TMN", "{\"id\":1,\"connect\":{\"ping\":25}}\n{}\n{\"push\":{\"channel\":\"public:trades-BTCIRT\",\"pub\":{\"data\":{\"price\":\"197700000000\",\"time\":1790000000000,\"type\":\"sell\",\"volume\":\"0.01\"}}}}", 19770000000, []string{"{}"}},
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
