package main

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"index-market/exchange"
)

// Seeded into SQLite on first start; after that the DB is the source of truth.
// Pairs are normalized BASE/QUOTE. TMN = toman.
var defaultPairs = map[string][]string{
	"binance": {"BTC/USDT", "ETH/USDT"},
	"kucoin":  {"BTC/USDT", "ETH/USDT"},
	"wallex":  {"BTC/USDT", "ETH/USDT", "USDT/TMN"},
	"nobitex": {"BTC/USDT", "ETH/USDT", "USDT/TMN"},
	"okx":     {"BTC/USDT", "ETH/USDT"},
	"bitget":  {"BTC/USDT", "ETH/USDT"},
	"bybit":   {"BTC/USDT", "ETH/USDT"},
}

// Symbols end up inside QuestDB SQL (no bind params there), so keep them strict.
var pairRe = regexp.MustCompile(`^[A-Z0-9]{1,15}/[A-Z0-9]{1,15}$`)

// streams owns the per-exchange pair lists (persisted in SQLite) and the
// running websocket stream for each exchange.
type streams struct {
	db    *sql.DB
	ctx   context.Context
	ticks chan<- exchange.Tick

	mu     sync.Mutex
	pairs  map[string][]string
	cancel map[string]context.CancelFunc
}

func openStreams(ctx context.Context, path string, ticks chan<- exchange.Tick) (*streams, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; avoids SQLITE_BUSY
	var exists int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='pairs'`).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		if _, err := db.Exec(`CREATE TABLE pairs (exchange TEXT NOT NULL, symbol TEXT NOT NULL, PRIMARY KEY (exchange, symbol))`); err != nil {
			return nil, err
		}
		for ex, ps := range defaultPairs {
			for _, p := range ps {
				if _, err := db.Exec(`INSERT INTO pairs VALUES (?, ?)`, ex, p); err != nil {
					return nil, err
				}
			}
		}
	}

	s := &streams{db: db, ctx: ctx, ticks: ticks, pairs: map[string][]string{}, cancel: map[string]context.CancelFunc{}}
	rows, err := db.Query(`SELECT exchange, symbol FROM pairs ORDER BY exchange, symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ex, sym string
		if err := rows.Scan(&ex, &sym); err != nil {
			return nil, err
		}
		s.pairs[ex] = append(s.pairs[ex], sym)
	}
	return s, rows.Err()
}

// startAll launches a stream for every exchange that has pairs.
func (s *streams) startAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ex := range exchange.All() {
		s.restart(ex)
	}
}

// restart stops ex's stream and starts it again with its current pairs. Caller holds mu.
func (s *streams) restart(ex exchange.Exchange) {
	if c := s.cancel[ex.Name]; c != nil {
		c()
		delete(s.cancel, ex.Name)
	}
	ps := s.pairs[ex.Name]
	if len(ps) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancel[ex.Name] = cancel
	go exchange.Run(ctx, ex, slices.Clone(ps), s.ticks)
}

// set adds (on=true) or removes a pair for one exchange, persists it and
// restarts that exchange's stream.
func (s *streams) set(exName, sym string, on bool) error {
	sym = strings.ToUpper(strings.TrimSpace(sym))
	if !pairRe.MatchString(sym) {
		return fmt.Errorf("bad symbol %q, want BASE/QUOTE", sym)
	}
	i := slices.IndexFunc(exchange.All(), func(e exchange.Exchange) bool { return e.Name == exName })
	if i < 0 {
		return fmt.Errorf("unknown exchange %q", exName)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := s.pairs[exName]
	has := slices.Contains(ps, sym)
	if has == on {
		return nil
	}
	if on {
		if _, err := s.db.Exec(`INSERT INTO pairs VALUES (?, ?)`, exName, sym); err != nil {
			return err
		}
		ps = append(slices.Clone(ps), sym)
		slices.Sort(ps)
	} else {
		if _, err := s.db.Exec(`DELETE FROM pairs WHERE exchange = ? AND symbol = ?`, exName, sym); err != nil {
			return err
		}
		ps = slices.DeleteFunc(slices.Clone(ps), func(p string) bool { return p == sym })
	}
	s.pairs[exName] = ps
	s.restart(exchange.All()[i])
	return nil
}

// snapshot returns a copy of every exchange's pairs.
func (s *streams) snapshot() map[string][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]string, len(s.pairs))
	for ex, ps := range s.pairs {
		if len(ps) > 0 {
			out[ex] = slices.Clone(ps)
		}
	}
	return out
}

// known reports whether any exchange tracks sym.
func (s *streams) known(sym string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ps := range s.pairs {
		if slices.Contains(ps, sym) {
			return true
		}
	}
	return false
}
