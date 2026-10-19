package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"index-market/exchange"
)

// QuestDB over its HTTP API: /exec for SQL, /write for ILP ingestion.
type questDB struct{ base string }

var httpc = &http.Client{Timeout: 10 * time.Second}

// prices: every raw trade tick. index_prices: every published index tick, the
// record a settlement dispute is checked against, so it's kept much longer.
// TTL drops whole partitions. ALTER applies the TTL to a table made by an older version.
var schema = []string{
	`CREATE TABLE IF NOT EXISTS prices (
		exchange SYMBOL, symbol SYMBOL, price DOUBLE, ts TIMESTAMP
	) TIMESTAMP(ts) PARTITION BY HOUR TTL 7 DAYS WAL`,
	`ALTER TABLE prices SET TTL 7 DAYS`,
	`CREATE TABLE IF NOT EXISTS index_prices (
		symbol SYMBOL, price DOUBLE, sources STRING, stale BOOLEAN, ts TIMESTAMP
	) TIMESTAMP(ts) PARTITION BY DAY TTL 90 DAYS WAL`,
}

// init retries until QuestDB is up (compose starts it alongside us).
func (q questDB) init(ctx context.Context) error {
	for _, sql := range schema {
		for {
			_, err := q.query(ctx, sql)
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("questdb not ready: %v", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
	return nil
}

// query runs SQL and returns rows as column-name -> value maps.
func (q questDB) query(ctx context.Context, sql string) ([]map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, q.base+"/exec?query="+url.QueryEscape(sql), nil)
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r struct {
		Error   string `json:"error"`
		Columns []struct {
			Name string `json:"name"`
		} `json:"columns"`
		Dataset [][]any `json:"dataset"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Error != "" {
		return nil, fmt.Errorf("questdb: %s", r.Error)
	}
	rows := make([]map[string]any, len(r.Dataset))
	for i, d := range r.Dataset {
		rows[i] = make(map[string]any, len(d))
		for j, c := range r.Columns {
			rows[i][c.Name] = d[j]
		}
	}
	return rows, nil
}

// Tag values are our own normalized names (BTC/USDT, binance): nothing to escape.
func tickLine(t exchange.Tick) string {
	return fmt.Sprintf("prices,exchange=%s,symbol=%s price=%s %d\n",
		t.Exchange, t.Symbol, strconv.FormatFloat(t.Price, 'f', -1, 64), t.Time.UnixNano())
}

func indexLine(ip indexPrice) string {
	price := ""
	if len(ip.Sources) > 0 {
		price = "price=" + strconv.FormatFloat(ip.Price, 'f', -1, 64) + ","
	}
	return fmt.Sprintf("index_prices,symbol=%s %ssources=%q,stale=%t %d\n",
		ip.Symbol, price, strings.Join(ip.Sources, ","), ip.Stale, ip.TS.UnixNano())
}

var dropped atomic.Int64

// send queues an ILP line without blocking: a slow QuestDB must never stall
// the exchange readers or the index loop. Dropped lines are counted and logged.
func send(lines chan<- string, l string) {
	select {
	case lines <- l:
	default:
		dropped.Add(1)
	}
}

// write batches ILP lines and flushes every second.
func (q questDB) write(ctx context.Context, lines <-chan string) {
	const maxBuf = 16 << 20
	var buf bytes.Buffer
	flush := time.NewTicker(time.Second)
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			// last flush, so a redeploy leaves no gap in index_prices
			if buf.Len() > 0 {
				c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := q.post(c, buf.Bytes()); err != nil {
					log.Printf("questdb final write: %v", err)
				}
				cancel()
			}
			return
		case l := <-lines:
			buf.WriteString(l)
		case <-flush.C:
			if n := dropped.Swap(0); n > 0 {
				log.Printf("questdb: dropped %d lines, writer behind", n)
			}
			if buf.Len() == 0 {
				continue
			}
			if err := q.post(ctx, buf.Bytes()); err != nil {
				log.Printf("questdb write: %v (%d bytes buffered)", err, buf.Len())
				// ponytail: drops whole buffer once DB is down too long; add disk spool if gaps matter
				if buf.Len() > maxBuf {
					buf.Reset()
				}
				continue
			}
			buf.Reset()
		}
	}
}

func (q questDB) post(ctx context.Context, body []byte) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/write", bytes.NewReader(body))
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("%s: %s", resp.Status, msg)
	}
	return nil
}
