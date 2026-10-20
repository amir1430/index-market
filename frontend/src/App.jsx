import { useState } from 'react'
import Chart from './Chart.jsx'
import { EXCHANGES, fmtAge, fmtPct, fmtPrice, usePoll } from './lib.js'

const RANGES = ['1h', '6h', '24h']

function readPref(k) {
  try {
    return localStorage.getItem(k)
  } catch {
    return null
  }
}
function writePref(k, v) {
  try {
    localStorage.setItem(k, v)
  } catch {}
}

export default function App() {
  const [data, err] = usePoll('/api/markets', 1000)
  const [selected, setSelected] = useState(readPref('symbol'))
  const now = Date.now()
  const staleMs = data?.maxAgeMs ?? 5000 // backend INDEX_MAX_AGE

  const markets = data?.markets ?? []
  const symbols = markets.map((m) => m.symbol) // MARKETS order
  const symbol = symbols.includes(selected) ? selected : symbols[0]

  const pick = (s) => {
    setSelected(s)
    writePref('symbol', s)
  }

  return (
    <main>
      <header>
        <div className="brand">
          <span className="logo" aria-hidden="true">
            <svg viewBox="0 0 20 20" fill="none" stroke="#fff" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 14l4-4 3 3 7-7" />
            </svg>
          </span>
          <div>
            <h1>Index Market</h1>
            <p className="sub">Volume-weighted index of Binance, OKX, Bybit, KuCoin, Bitget, Gate and MEXC · sources silent &gt;{fmtAge(staleMs)} or off the median excluded</p>
          </div>
        </div>
        <div className="header-right">
          <ExchangeStatus markets={markets} now={now} />
          <ThemeToggle />
        </div>
      </header>

      {err && <p className="banner">Can't reach API ({err}) — showing last known data.</p>}
      {!data && !err && <p className="muted">Loading…</p>}

      <section className="cards">
        {markets.map((m) => (
          <Card key={m.symbol} market={m} minSources={data.minSources} now={now} on={m.symbol === symbol} onClick={() => pick(m.symbol)} />
        ))}
      </section>

      {symbol && <History symbol={symbol} />}
    </main>
  )
}

function ExchangeStatus({ markets, now }) {
  const rows = markets.flatMap((m) => m.exchanges)
  return (
    <ul className="exstatus" aria-label="Exchange status">
      {EXCHANGES.map((ex) => {
        const mine = rows.filter((r) => r.exchange === ex)
        const newest = Math.max(...mine.map((r) => Date.parse(r.ts)))
        const state = !mine.length ? 'offline' : mine.some((r) => r.fresh) ? 'live' : 'stale'
        return (
          <li key={ex} className={state} title={isFinite(newest) ? `last trade ${fmtAge(now - newest)} ago` : 'no data'}>
            <i className="dot" />
            {ex}
            <span className="muted">{state}</span>
          </li>
        )
      })}
    </ul>
  )
}

function ThemeToggle() {
  const [theme, setTheme] = useState(() => {
    const t = readPref('theme')
    if (t) document.documentElement.dataset.theme = t
    return t ?? (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
  })
  const next = theme === 'dark' ? 'light' : 'dark'
  return (
    <button
      className="ghost"
      onClick={() => {
        document.documentElement.dataset.theme = next
        writePref('theme', next)
        setTheme(next)
      }}
      aria-label={`Switch to ${next} theme`}
    >
      {theme === 'dark' ? '☀︎' : '☾'}
    </button>
  )
}

function Card({ market, minSources, now, on, onClick }) {
  const { symbol, index, exchanges } = market
  const n = index.sources.length
  const live = exchanges.filter((r) => r.fresh).map((r) => r.price)
  const multi = n > 1 // deviation is meaningless with one source
  const spread = live.length > 1 ? (Math.max(...live) - Math.min(...live)) / index.price : null
  return (
    <button className={on ? 'card on' : 'card'} onClick={onClick} aria-pressed={on}>
      <div className="card-head">
        <h2>{symbol}</h2>
        <span className="muted">
          {n ? `${n} source${n > 1 ? 's' : ''}` : 'no live source'}
          {spread != null && ` · spread ${fmtPct(spread).replace('+', '')}`}
        </span>
      </div>
      <div className="hero num">{n ? fmtPrice(index.price, symbol) : '—'}</div>
      {index.stale && <p className="banner">Stale: fewer than {minSources} live sources, not settled on</p>}
      <table>
        <tbody>
          {exchanges.map((r) => (
            <tr
              key={r.exchange}
              className={r.weight > 0 ? '' : 'stale'}
              title={r.weight > 0 ? undefined : r.fresh ? 'excluded from index: too far from the median' : 'excluded from index: stale'}
            >
              <td>
                <i className="swatch" style={{ background: `var(--${r.exchange})` }} />
                {r.exchange}
              </td>
              <td className="num">{fmtPrice(r.price, symbol)}</td>
              {multi && <td className="num dev">{fmtPct(r.price / index.price - 1)}</td>}
              <td className="num weight" title="share of the index">{r.weight > 0 ? `${Math.round(r.weight * 100)}%` : '—'}</td>
              <td className="num muted age">{fmtAge(now - Date.parse(r.ts))}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </button>
  )
}

function History({ symbol }) {
  const [range, setRange] = useState(() => (RANGES.includes(readPref('range')) ? readPref('range') : '6h'))
  const [rows, err, loading] = usePoll(`/api/history?symbol=${encodeURIComponent(symbol)}&range=${range}`, range === '1h' ? 10_000 : 30_000)
  return (
    <section className="panel" aria-busy={loading}>
      <div className="panel-head">
        <h2>
          {symbol} <span className="muted">· index vs exchanges</span>
        </h2>
        <div className="seg" role="group" aria-label="Time range">
          {RANGES.map((r) => (
            <button
              key={r}
              aria-pressed={r === range}
              onClick={() => {
                setRange(r)
                writePref('range', r)
              }}
            >
              {r}
            </button>
          ))}
        </div>
      </div>
      {err && <p className="banner">History error: {err}</p>}
      {rows ? <Chart rows={rows} symbol={symbol} /> : <div className="chart" />}
    </section>
  )
}
