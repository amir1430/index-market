import { useLayoutEffect, useRef, useState } from 'react'
import { EXCHANGES, fmtPct, fmtPrice } from './lib.js'

const H = 340
const M = { top: 12, right: 84, bottom: 28 } // left is sized to the y labels

// rows: [{ts, exchange, price}] per bucket; exchange "index" is the median line.
export default function Chart({ rows, symbol }) {
  const wrap = useRef(null)
  const [w, setW] = useState(800)
  const [hover, setHover] = useState(null) // index into times
  useLayoutEffect(() => {
    setW(wrap.current.clientWidth) // before first paint; observer fires async
    const ro = new ResizeObserver(([e]) => setW(e.contentRect.width))
    ro.observe(wrap.current)
    return () => ro.disconnect()
  }, [])

  // time -> {series: price}
  const byTime = new Map()
  for (const r of rows) {
    if (r.price == null) continue
    const t = Date.parse(r.ts)
    if (!byTime.has(t)) byTime.set(t, {})
    byTime.get(t)[r.exchange] = r.price
  }
  const times = [...byTime.keys()].sort((a, b) => a - b)
  const exchanges = EXCHANGES.filter((ex) => rows.some((r) => r.exchange === ex && r.price != null))
  const series = ['index', ...exchanges]

  if (times.length < 2)
    return (
      <div ref={wrap} className="chart">
        <p className="muted empty">Not enough history yet — check back in a minute.</p>
      </div>
    )

  const prices = [...byTime.values()].flatMap(Object.values)
  let lo = Math.min(...prices), hi = Math.max(...prices)
  const pad = (hi - lo) * 0.08 || hi * 0.001
  lo -= pad
  hi += pad
  const t0 = times[0], t1 = times[times.length - 1]

  // y labels: decimals from tick step so neighbours never print the same
  const yTicks = Array.from({ length: 5 }, (_, i) => lo + ((hi - lo) * i) / 4)
  const dec = Math.min(8, Math.max(0, Math.ceil(-Math.log10((hi - lo) / 4))))
  const yLabel = (v) => v.toLocaleString('en-US', { minimumFractionDigits: dec, maximumFractionDigits: dec })
  const left = Math.max(...yTicks.map((v) => yLabel(v).length)) * 6.5 + 14
  const iw = w - left - M.right, ih = H - M.top - M.bottom
  const x = (t) => left + ((t - t0) / (t1 - t0)) * iw
  const y = (p) => M.top + (1 - (p - lo) / (hi - lo)) * ih

  // Paths break on gaps instead of bridging them.
  const path = (s) => {
    let d = '', pen = false
    for (const t of times) {
      const p = byTime.get(t)[s]
      if (p == null) {
        pen = false
        continue
      }
      d += `${pen ? 'L' : 'M'}${x(t).toFixed(1)},${y(p).toFixed(1)}`
      pen = true
    }
    return d
  }

  // Direct labels at each line's last point, nudged apart so they don't collide.
  const labels = series
    .map((s) => ({ s, y: y(byTime.get(times.findLast((t) => byTime.get(t)[s] != null))[s]) }))
    .sort((a, b) => a.y - b.y)
  for (let i = 1; i < labels.length; i++) labels[i].y = Math.max(labels[i].y, labels[i - 1].y + 14)

  // smallest step that keeps x-axis to <= 8 labels
  const step = [5, 15, 30, 60, 180].map((m) => m * 60_000).find((s) => (t1 - t0) / s <= 8) ?? 3 * 3600_000
  const xTicks = []
  for (let t = Math.ceil(t0 / step) * step; t <= t1; t += step) xTicks.push(t)

  const onMove = (e) => {
    const px = e.clientX - e.currentTarget.getBoundingClientRect().left
    const t = t0 + ((px - left) / iw) * (t1 - t0)
    let a = 0, b = times.length - 1
    while (a < b) {
      const mid = (a + b) >> 1
      times[mid] < t ? (a = mid + 1) : (b = mid)
    }
    if (a > 0 && t - times[a - 1] < times[a] - t) a--
    setHover(a)
  }

  const ht = hover != null ? times[hover] : null
  const hx = ht != null ? x(ht) : 0
  const hv = ht != null ? byTime.get(ht) : null
  const color = (s) => `var(--${s})`

  return (
    <div ref={wrap} className="chart">
      <ul className="legend">
        {series.map((s) => (
          <li key={s}>
            <i className={s === 'index' ? 'swatch line dashed' : 'swatch line'} style={{ '--c': color(s) }} />
            {s}
          </li>
        ))}
      </ul>
      <svg width={w} height={H} onMouseMove={onMove} onMouseLeave={() => setHover(null)} role="img" aria-label={`${symbol} index and exchange prices`}>
        {yTicks.map((v) => (
          <g key={v}>
            <line className="grid" x1={left} x2={left + iw} y1={y(v)} y2={y(v)} />
            <text className="tick" x={left - 8} y={y(v)} dy="0.32em" textAnchor="end">{yLabel(v)}</text>
          </g>
        ))}
        {xTicks.map((t) => (
          <text key={t} className="tick" x={x(t)} y={H - 8} textAnchor="middle">
            {new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
          </text>
        ))}
        {exchanges.map((s) => (
          <path key={s} d={path(s)} fill="none" stroke={color(s)} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
        ))}
        {/* index on top: ink, dashed so it reads as derived, not another venue */}
        <path d={path('index')} fill="none" stroke={color('index')} strokeWidth="2" strokeDasharray="6 4" strokeLinejoin="round" />
        {labels.map((l) => (
          <text key={l.s} className={l.s === 'index' ? 'label strong' : 'label'} x={left + iw + 8} y={l.y} dy="0.32em">{l.s}</text>
        ))}
        {ht != null && (
          <g>
            <line className="crosshair" x1={hx} x2={hx} y1={M.top} y2={M.top + ih} />
            {series.map((s) =>
              hv[s] == null ? null : <circle key={s} cx={hx} cy={y(hv[s])} r="4" fill={color(s)} stroke="var(--surface)" strokeWidth="2" />,
            )}
          </g>
        )}
      </svg>
      {ht != null && (
        <div className="tooltip" style={hx > w / 2 ? { right: w - hx + 12 } : { left: hx + 12 }}>
          <div className="muted">{new Date(ht).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })}</div>
          {series.map((s) =>
            hv[s] == null ? null : (
              <div key={s} className={s === 'index' ? 'row strong' : 'row'}>
                <i className="swatch" style={{ background: color(s) }} />
                <span>{s}</span>
                <b className="num">{fmtPrice(hv[s], symbol)}</b>
                <em className="num">{s !== 'index' && hv.index ? fmtPct(hv[s] / hv.index - 1) : ''}</em>
              </div>
            ),
          )}
        </div>
      )}
    </div>
  )
}
