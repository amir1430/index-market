package exchange

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestParse(t *testing.T) {
	cases := []struct {
		ex      Exchange
		pair    string
		msg     string
		price   float64
		size    float64
		replies []string
	}{
		{Binance, "BTC/USDT", `{"stream":"btcusdt@trade","data":{"e":"trade","s":"BTCUSDT","p":"84732.21","q":"0.25","T":1790000000000}}`, 84732.21, 0.25, nil},
		{OKX, "BTC/USDT", `{"arg":{"channel":"trades","instId":"BTC-USDT"},"data":[{"instId":"BTC-USDT","tradeId":"1","px":"84730.1","sz":"0.1","side":"buy","ts":"1790000000000"}]}`, 84730.1, 0.1, nil},
		{OKX, "BTC/USDT", `pong`, 0, 0, nil},
		{Bybit, "BTC/USDT", `{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1790000000001,"data":[{"T":1790000000000,"s":"BTCUSDT","S":"Buy","v":"0.001","p":"84729.9","L":"PlusTick","i":"1","BT":false}]}`, 84729.9, 0.001, nil},
		{Bybit, "BTC/USDT", `{"success":true,"ret_msg":"pong","conn_id":"x","op":"ping"}`, 0, 0, nil},
		{KuCoin, "BTC/USDT", `{"type":"message","topic":"/market/match:BTC-USDT","subject":"trade.l3match","data":{"makerOrderId":"a","price":"84731.5","sequence":"1","side":"buy","size":"0.0042","symbol":"BTC-USDT","takerOrderId":"b","time":"1790000000000123456","tradeId":"1","type":"match"}}`, 84731.5, 0.0042, nil},
		{KuCoin, "BTC/USDT", `{"id":"x","type":"welcome"}`, 0, 0, nil},
		{KuCoin, "BTC/USDT", `{"id":"1","type":"pong"}`, 0, 0, nil},
		{Bitget, "BTC/USDT", `{"action":"update","arg":{"instType":"SPOT","channel":"trade","instId":"BTCUSDT"},"data":[{"ts":"1790000000000","price":"84728.4","size":"0.03","side":"buy","tradeId":"1"}],"ts":1790000000001}`, 84728.4, 0.03, nil},
		{Bitget, "BTC/USDT", `{"action":"snapshot","arg":{"instType":"SPOT","channel":"trade","instId":"BTCUSDT"},"data":[{"ts":"1790000000000","price":"1","size":"9","side":"buy","tradeId":"1"}]}`, 0, 0, nil},
		{Bitget, "BTC/USDT", `pong`, 0, 0, nil},
		{Gate, "BTC/USDT", `{"time":1790000000,"time_ms":1790000000001,"channel":"spot.trades","event":"update","result":{"id":1,"create_time":1790000000,"create_time_ms":"1790000000000.123","side":"buy","currency_pair":"BTC_USDT","amount":"0.0015","price":"84727.3","range":"1-1"}}`, 84727.3, 0.0015, nil},
		{Gate, "BTC/USDT", `{"time":1790000000,"channel":"spot.pong","event":"","result":null}`, 0, 0, nil},
		{Gate, "BTC/USDT", `{"time":1790000000,"channel":"spot.trades","event":"subscribe","payload":["BTC_USDT"],"result":{"status":"success"}}`, 0, 0, nil},
		{MEXC, "BTC/USDT", string(mexcDeal("BTCUSDT", "84726.1", "0.002", 1790000000000)), 84726.1, 0.002, nil},
		{MEXC, "BTC/USDT", `{"id":0,"code":0,"msg":"PONG"}`, 0, 0, nil},
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
		if len(ticks) != 1 || ticks[0].Symbol != c.pair || ticks[0].Price != c.price || ticks[0].Size != c.size || ticks[0].Time.Before(time.Unix(1e9, 0)) || ticks[0].Time.After(time.Unix(2e9, 0)) {
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
		{KuCoin, `{"id":"1","type":"error","code":404,"data":"topic /market/match:NOPE-USDT is not found"}`},
		{Bitget, `{"event":"error","arg":{"instType":"SPOT","channel":"trade","instId":"TONUSDT"},"code":30001,"msg":"instId:TONUSDT doesn't exist"}`},
		{Gate, `{"time":1790000000,"channel":"spot.trades","event":"subscribe","payload":["NOPE_USDT"],"error":{"code":2,"message":"unknown currency pair: NOPE_USDT"},"result":{"status":"fail"}}`},
		{MEXC, `{"id":0,"code":1,"msg":"Not Subscribed successfully! [spot@public.aggre.deals.v3.api.pb@100ms@NOPEUSDT]"}`},
	} {
		if _, _, err := c.ex.Parse([]byte(c.msg), nil); err == nil {
			t.Errorf("%s: refused subscribe not reported", c.ex.Name)
		}
		if n := len(c.ex.Subscribe([]string{"A", "B", "C"})); n != 3 {
			t.Errorf("%s: %d subscribe requests for 3 markets, want one each", c.ex.Name, n)
		}
	}
}

// mexcDeal encodes one aggregated trade as MEXC's PushDataV3ApiWrapper.
func mexcDeal(symbol, price, qty string, ms uint64) []byte {
	var item, deals, wrap []byte
	item = protowire.AppendTag(item, 1, protowire.BytesType)
	item = protowire.AppendString(item, price)
	item = protowire.AppendTag(item, 2, protowire.BytesType)
	item = protowire.AppendString(item, qty)
	item = protowire.AppendTag(item, 3, protowire.VarintType)
	item = protowire.AppendVarint(item, 1)
	item = protowire.AppendTag(item, 4, protowire.VarintType)
	item = protowire.AppendVarint(item, ms)
	deals = protowire.AppendTag(deals, 1, protowire.BytesType)
	deals = protowire.AppendBytes(deals, item)
	deals = protowire.AppendTag(deals, 2, protowire.BytesType)
	deals = protowire.AppendString(deals, "spot@public.aggre.deals.v3.api.pb@100ms")
	wrap = protowire.AppendTag(wrap, 1, protowire.BytesType)
	wrap = protowire.AppendString(wrap, "spot@public.aggre.deals.v3.api.pb@100ms@"+symbol)
	wrap = protowire.AppendTag(wrap, 314, protowire.BytesType)
	wrap = protowire.AppendBytes(wrap, deals)
	wrap = protowire.AppendTag(wrap, 3, protowire.BytesType)
	wrap = protowire.AppendString(wrap, symbol)
	wrap = protowire.AppendTag(wrap, 6, protowire.VarintType)
	wrap = protowire.AppendVarint(wrap, ms+5)
	return wrap
}
