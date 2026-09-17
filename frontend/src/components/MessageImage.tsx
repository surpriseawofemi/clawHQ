import { useEffect, useState } from 'react'
import { api } from '../api'
import type { ChatMessage } from '../types'

export type ImageBlock = { artifactId: string; title?: string; mimeType?: string }

/** Image attachments an agent put on a reply: the gateway stores them as artifacts. */
export function imageBlocks(msg: ChatMessage): ImageBlock[] {
  if (!Array.isArray(msg.content)) return []
  const out: ImageBlock[] = []
  for (const b of msg.content) {
    if (b && b.type === 'image' && typeof b.artifactId === 'string') {
      out.push({ artifactId: b.artifactId, title: typeof b.title === 'string' ? b.title : undefined, mimeType: typeof b.mimeType === 'string' ? b.mimeType : undefined })
    }
  }
  return out
}

/** The legacy "MEDIA:<path>" line is delivery metadata, not prose. */
export function stripMediaLines(text: string): string {
  return text.replace(/^\s*MEDIA:.*$/gm, '').replace(/\n{3,}/g, '\n\n').trim()
}

const inflight = new Map<string, Promise<string>>()
const loaded = new Map<string, string>()

function fetchImage(artifactId: string, sessionKey: string): Promise<string> {
  const hit = loaded.get(artifactId)
  if (hit) return Promise.resolve(hit)
  let p = inflight.get(artifactId)
  if (!p) {
    p = api.media
      .fetch(artifactId, sessionKey)
      .then((url) => {
        loaded.set(artifactId, url)
        return url
      })
      .finally(() => inflight.delete(artifactId))
    inflight.set(artifactId, p)
  }
  return p
}

/**
 * One attached image: fetched through the gateway as a data URL (Go keeps a
 * cache), shown inline, click for full size.
 */
export function MessageImage({ artifactId, sessionKey, title }: { artifactId: string; sessionKey: string; title?: string }): React.JSX.Element {
  const [src, setSrc] = useState<string | null>(loaded.get(artifactId) ?? null)
  const [error, setError] = useState<string | null>(null)
  const [zoom, setZoom] = useState(false)
  const [tries, setTries] = useState(0)

  useEffect(() => {
    let alive = true
    fetchImage(artifactId, sessionKey)
      .then((url) => {
        if (alive) setSrc(url)
      })
      .catch((err) => {
        if (alive) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      alive = false
    }
  }, [artifactId, sessionKey, tries])

  if (error) {
    return (
      <div className="msg-image-error">
        <span>🖼 {title ?? 'image'}: {error}</span>
        <button className="btn btn-sm btn-ghost" onClick={() => { setError(null); setTries((n) => n + 1) }}>
          Retry
        </button>
      </div>
    )
  }
  if (!src) return <div className="msg-image-loading">🖼 Loading {title ?? 'image'}…</div>
  return (
    <>
      <img className="msg-image" src={src} alt={title ?? 'attachment'} title={`${title ?? 'image'} · click to enlarge`} onClick={() => setZoom(true)} />
      {zoom && (
        <div className="lightbox" onClick={() => setZoom(false)} role="dialog" aria-label={title ?? 'image'}>
          <img src={src} alt={title ?? 'attachment'} />
          <div className="lightbox-bar">
            <span>{title ?? 'image'}</span>
            <button className="btn btn-sm" onClick={(e) => { e.stopPropagation(); setZoom(false) }}>Close</button>
          </div>
        </div>
      )}
    </>
  )
}
