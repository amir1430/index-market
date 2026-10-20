package exchange

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// MEXC v3 spot aggregated trades. Data frames are protobuf (MEXC retired the JSON
// streams); acks and pongs are JSON. Messages from MEXC's websocket-proto:
//
//	PushDataV3ApiWrapper  { channel = 1; symbol = 3; publicAggreDeals = 314; }
//	PublicAggreDealsV3Api { repeated deals = 1; eventType = 2; }
//	PublicAggreDealsV3ApiItem { price = 1; quantity = 2; tradeType = 3; time = 4 (ms); }
var MEXC = Exchange{
	Name:   "mexc",
	Native: concat,
	URL:    staticURL("wss://wbs-api.mexc.com/ws"),
	Subscribe: func(natives []string) [][]byte {
		// One request per market, so an unknown one can't take the others down with it.
		// ponytail: MEXC allows 30 subscriptions per connection; split connections past that
		msgs := make([][]byte, len(natives))
		for i, n := range natives {
			msgs[i] = fmt.Appendf(nil, `{"method":"SUBSCRIPTION","params":["spot@public.aggre.deals.v3.api.pb@100ms@%s"]}`, n)
		}
		return msgs
	},
	Ping:      func() []byte { return []byte(`{"method":"PING"}`) },
	PingEvery: 20 * time.Second,
	Parse: func(msg []byte, syms map[string]string) ([]Tick, [][]byte, error) {
		if len(msg) > 0 && msg[0] == '{' {
			var m struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(msg, &m); err != nil {
				return nil, nil, err
			}
			if m.Code != 0 {
				return nil, nil, fmt.Errorf("refused: %s", m.Msg)
			}
			return nil, nil, nil // subscribe ack, PONG
		}
		var symbol string
		var deals []byte
		err := fields(msg, func(num protowire.Number, v []byte) error {
			switch num {
			case 3:
				symbol = string(v)
			case 314:
				deals = v
			}
			return nil
		})
		if err != nil || deals == nil {
			return nil, nil, err
		}
		var ticks []Tick
		err = fields(deals, func(num protowire.Number, item []byte) error {
			if num != 1 {
				return nil
			}
			var price, qty, ms string
			if err := fields(item, func(num protowire.Number, v []byte) error {
				switch num {
				case 1:
					price = string(v)
				case 2:
					qty = string(v)
				case 4:
					n, _ := protowire.ConsumeVarint(v)
					ms = strconv.FormatUint(n, 10)
				}
				return nil
			}); err != nil {
				return err
			}
			t, err := tradeTick(syms, symbol, price, qty, ms)
			ticks = append(ticks, t...)
			return err
		})
		return ticks, nil, err
	},
}

// fields calls f with every field of a protobuf message: a length-delimited
// field's payload, or a varint field's raw bytes (decode with ConsumeVarint).
func fields(b []byte, f func(protowire.Number, []byte) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return errors.New("protobuf: bad tag")
		}
		b = b[n:]
		var v []byte
		switch typ {
		case protowire.BytesType:
			v, n = protowire.ConsumeBytes(b)
		case protowire.VarintType:
			_, n = protowire.ConsumeVarint(b)
			if n >= 0 {
				v = b[:n]
			}
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
		}
		if n < 0 {
			return errors.New("protobuf: bad field")
		}
		b = b[n:]
		if v != nil {
			if err := f(num, v); err != nil {
				return err
			}
		}
	}
	return nil
}
