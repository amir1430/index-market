import { useEffect, useState } from 'react'

// Fixed order = fixed color slot per exchange (color follows the entity).
export const EXCHANGES = ['binance', 'kucoin', 'wallex', 'nobitex']

export function fmtPrice(p, symbol) {
  const digits = symbol.endsWith('/TMN') ? 0 : p < 10 ? 4 : 2
  return p.toLocaleString('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits })
}

export function fmtPct(x) {
  const s = (x * 100).toFixed(Math.abs(x) < 0.001 ? 3 : 2)
  return (x > 0 ? '+' : x < 0 ? '' : '±') + s + '%'
}

export function fmtAge(ms) {
  const s = Math.max(0, Math.round(ms / 1000))
  return s < 60 ? `${s}s` : s < 3600 ? `${Math.floor(s / 60)}m` : `${Math.floor(s / 3600)}h`
}

// Polls url every `ms`; returns [data, error]. Keeps last data on error.
export function usePoll(url, ms) {
  const [state, setState] = useState([null, null])
  useEffect(() => {
    let alive = true
    const load = () =>
      fetch(url)
        .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`${r.status} ${r.statusText}`))))
        .then((d) => alive && setState([d, null]))
        .catch((e) => alive && setState((s) => [s[0], e.message]))
    load()
    const id = setInterval(load, ms)
    return () => {
      alive = false
      clearInterval(id)
    }
  }, [url, ms])
  return state
}
