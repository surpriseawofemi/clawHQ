import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * Keeps a page's data live without relying on gateway events: plugin state can
 * change in a process ClawHQ never hears from (the tool bridge for CLI agents),
 * so pages poll while visible, refresh when the window comes back to the front,
 * and offer a button for right now.
 */
export function useLiveRefresh(refresh: () => Promise<unknown>, everyMs = 8000): { busy: boolean; now: () => void } {
  const [busy, setBusy] = useState(false)
  const inFlight = useRef(false)
  const run = useCallback(async () => {
    if (inFlight.current) return
    inFlight.current = true
    setBusy(true)
    try {
      await refresh()
    } finally {
      inFlight.current = false
      setBusy(false)
    }
  }, [refresh])

  useEffect(() => {
    const tick = setInterval(() => {
      if (document.visibilityState === 'visible') void run()
    }, everyMs)
    const onFocus = (): void => void run()
    const onVisible = (): void => {
      if (document.visibilityState === 'visible') void run()
    }
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      clearInterval(tick)
      window.removeEventListener('focus', onFocus)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [run, everyMs])

  return { busy, now: () => void run() }
}
