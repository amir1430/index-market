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
	"time"

	"index-market/exchange"
)

// QuestDB over its HTTP API: /exec for SQL, /write for ILP ingestion.
type questDB struct{ base string }

// PARTITION BY HOUR + TTL 1 DAY: QuestDB drops whole partitions older than a day.
const schema = `CREATE TABLE IF NOT EXISTS prices (
	exchange SYMBOL, symbol SYMBOL, price DOUBLE, ts TIMESTAMP
) TIMESTAMP(ts) PARTITION BY HOUR TTL 1 DAY WAL`

// init retries until QuestDB is up (compose starts it alongside us).
func (q questDB) init(ctx context.Context) error {
	for {
		_, err := q.query(ctx, schema)
		if err == nil || ctx.Err() != nil {
			return err
		}
		log.Printf("questdb not ready: %v", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// query runs SQL and returns rows as column-name -> value maps.
func (q questDB) query(ctx context.Context, sql string) ([]map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, q.base+"/exec?query="+url.QueryEscape(sql), nil)
	resp, err := http.DefaultClient.Do(req)
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

// write batches ticks as ILP lines and flushes every second.
func (q questDB) write(ctx context.Context, ticks <-chan exchange.Tick) {
	const maxBuf = 16 << 20
	var buf bytes.Buffer
	flush := time.NewTicker(time.Second)
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticks:
			// tag values are our own normalized names: no spaces/commas/= to escape
			fmt.Fprintf(&buf, "prices,exchange=%s,symbol=%s price=%s %d\n",
				t.Exchange, t.Symbol, strconv.FormatFloat(t.Price, 'f', -1, 64), t.Time.UnixNano())
		case <-flush.C:
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
	resp, err := http.DefaultClient.Do(req)
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
