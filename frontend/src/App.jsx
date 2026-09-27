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
  const [data, err] = usePoll('/api/latest', 2000)
  const [selected, setSelected] = useState(readPref('symbol'))
  const now = Date.now()
  const staleMs = data?.maxAgeMs ?? 120_000 // backend INDEX_MAX_AGE

  const prices = data?.prices ?? []
  const index = Object.fromEntries((data?.index ?? []).map((i) => [i.symbol, i]))
  const bySymbol = {}
  for (const r of prices) (bySymbol[r.symbol] ??= []).push(r)
  const symbols = Object.keys(bySymbol).sort((a, b) => bySymbol[b].length - bySymbol[a].length || a.localeCompare(b))
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
            <p className="sub">Median index price across exchanges · stale sources (&gt;{fmtAge(staleMs)}) excluded</p>
          </div>
        </div>
        <div className="header-right">
          <ExchangeStatus prices={prices} now={now} staleMs={staleMs} />
          <ThemeToggle />
        </div>
      </header>

      {err && <p className="banner">Can't reach API ({err}) — showing last known data.</p>}
      {!data && !err && <p className="muted">Loading…</p>}
      {data && symbols.length === 0 && <p className="muted">No prices yet — exchanges are connecting.</p>}

      <section className="cards">
        {symbols.map((s) => (
          <Card key={s} symbol={s} rows={bySymbol[s]} index={index[s]} now={now} staleMs={staleMs} on={s === symbol} onClick={() => pick(s)} />
        ))}
      </section>

      {symbol && <History key={symbol} symbol={symbol} />}
    </main>
  )
}

function ExchangeStatus({ prices, now, staleMs }) {
  return (
    <ul className="exstatus" aria-label="Exchange status">
      {EXCHANGES.map((ex) => {
        const newest = Math.max(...prices.filter((r) => r.exchange === ex).map((r) => Date.parse(r.ts)))
        const state = !isFinite(newest) ? 'offline' : now - newest > staleMs ? 'stale' : 'live'
        return (
          <li key={ex} className={state} title={isFinite(newest) ? `last tick ${fmtAge(now - newest)} ago` : 'no data'}>
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

function Card({ symbol, rows, index, now, staleMs, on, onClick }) {
  const ordered = EXCHANGES.map((ex) => rows.find((r) => r.exchange === ex)).filter(Boolean)
  const live = ordered.filter((r) => now - Date.parse(r.ts) <= staleMs).map((r) => r.price)
  const multi = index?.sources.length > 1 // deviation is meaningless with one source
  const spread = index && live.length > 1 ? (Math.max(...live) - Math.min(...live)) / index.price : null
  return (
    <button className={on ? 'card on' : 'card'} onClick={onClick} aria-pressed={on}>
      <div className="card-head">
        <h2>{symbol}</h2>
        <span className="muted">
          {index ? `${index.sources.length} source${index.sources.length > 1 ? 's' : ''}` : 'no live source'}
          {spread != null && ` · spread ${fmtPct(spread).replace('+', '')}`}
        </span>
      </div>
      <div className="hero num">{index ? fmtPrice(index.price, symbol) : '—'}</div>
      <table>
        <tbody>
          {ordered.map((r) => {
            const stale = now - Date.parse(r.ts) > staleMs
            return (
              <tr key={r.exchange} className={stale ? 'stale' : ''} title={stale ? 'excluded from index: stale' : undefined}>
                <td>
                  <i className="swatch" style={{ background: `var(--${r.exchange})` }} />
                  {r.exchange}
                </td>
                <td className="num">{fmtPrice(r.price, symbol)}</td>
                {multi && <td className="num dev">{fmtPct(r.price / index.price - 1)}</td>}
                <td className="num muted age">{fmtAge(now - Date.parse(r.ts))}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </button>
  )
}

function History({ symbol }) {
  const [range, setRange] = useState(() => (RANGES.includes(readPref('range')) ? readPref('range') : '6h'))
  const [rows, err] = usePoll(`/api/history?symbol=${encodeURIComponent(symbol)}&range=${range}`, range === '1h' ? 10_000 : 30_000)
  return (
    <section className="panel">
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
      {rows && <Chart key={range} rows={rows} symbol={symbol} />}
    </section>
  )
}
