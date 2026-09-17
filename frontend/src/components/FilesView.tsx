import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import { renderMarkdown } from '../markdown'
import type { DirListing, FileContent, FileEntry, Transfer } from '../types'

const sizeOf = (n: number): string => (n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : n < 1073741824 ? `${(n / 1048576).toFixed(1)} MB` : `${(n / 1073741824).toFixed(2)} GB`)
const when = (ms: number): string => new Date(ms).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
const iconOf = (e: FileEntry): string => {
  if (e.isDir) return '📁'
  const ext = e.name.split('.').pop()?.toLowerCase() ?? ''
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg'].includes(ext)) return '🖼'
  if (['js', 'ts', 'tsx', 'jsx', 'py', 'go', 'rb', 'php', 'sh', 'rs', 'java', 'c', 'cpp', 'h'].includes(ext)) return '📜'
  if (['md', 'txt', 'log'].includes(ext)) return '📄'
  if (['json', 'yml', 'yaml', 'toml', 'env', 'ini', 'conf'].includes(ext)) return '⚙️'
  if (['zip', 'gz', 'tar', 'tgz', 'bz2', 'xz', '7z'].includes(ext)) return '🗜'
  return '📃'
}

/**
 * Files on a server over SFTP: browse, open text files in an editor and save
 * them back, preview images and Markdown, upload with the native picker,
 * download, rename, delete, new folder or file. The server keeps one SFTP
 * connection open while you work.
 */
type Ask = { kind: 'prompt' | 'confirm'; title: string; value: string; resolve: (v: string | null) => void }

export function FilesView({ serverId, startDir }: { serverId: string; startDir?: string }): React.JSX.Element {
  // The webview has no native confirm/prompt, so dialogs are drawn in the page.
  const [ask, setAsk] = useState<Ask | null>(null)
  const prompt = (title: string, initial = ''): Promise<string | null> => new Promise((resolve) => setAsk({ kind: 'prompt', title, value: initial, resolve }))
  const confirm = (title: string): Promise<boolean> => new Promise((resolve) => setAsk({ kind: 'confirm', title, value: '', resolve: (v) => resolve(v !== null) }))
  const settle = (v: string | null): void => {
    ask?.resolve(v)
    setAsk(null)
  }
  const [listing, setListing] = useState<DirListing | null>(null)
  const [dir, setDir] = useState(startDir ?? '')
  const [open, setOpen] = useState<FileContent | null>(null)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [transfers, setTransfers] = useState<Record<string, Transfer>>({})
  const [filter, setFilter] = useState('')
  const [pathInput, setPathInput] = useState('')
  const editor = useRef<HTMLTextAreaElement>(null)

  const load = useCallback(
    async (d: string) => {
      setBusy('list')
      setError(null)
      try {
        const res = await api.files.list(serverId, d)
        setListing(res)
        setDir(res.path)
        setPathInput(res.path)
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        setBusy(null)
      }
    },
    [serverId]
  )
  useEffect(() => {
    void load(startDir ?? '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId])

  useEffect(
    () =>
      api.onFileProgress((t) => {
        if (t.serverId !== serverId) return
        setTransfers((prev) => ({ ...prev, [t.name]: t }))
        if (t.finished && !t.error) {
          setTimeout(() => setTransfers((prev) => { const n = { ...prev }; delete n[t.name]; return n }), 2500)
          void load(dir)
        }
      }),
    [serverId, dir, load]
  )

  const dirty = open?.isText && draft !== (open.text ?? '')

  const openFile = async (e: FileEntry): Promise<void> => {
    if (dirty && !(await confirm('Discard unsaved changes?'))) return
    if (e.isDir) {
      setOpen(null)
      await load(e.path)
      return
    }
    setBusy(`open:${e.path}`)
    setError(null)
    try {
      const f = await api.files.read(serverId, e.path)
      setOpen(f)
      setDraft(f.text ?? '')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const save = async (): Promise<void> => {
    if (!open || !open.isText) return
    setBusy('save')
    setError(null)
    try {
      const f = await api.files.write(serverId, open.path, draft)
      setOpen(f)
      setDraft(f.text ?? '')
      void load(dir)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const act = async (label: string, fn: () => Promise<unknown>): Promise<void> => {
    setBusy(label)
    setError(null)
    try {
      await fn()
      await load(dir)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const crumbs = useMemo(() => {
    if (!listing) return []
    const parts = listing.path.split('/').filter(Boolean)
    const out: { label: string; path: string }[] = [{ label: '/', path: '/' }]
    let acc = ''
    for (const p of parts) {
      acc += `/${p}`
      out.push({ label: p, path: acc })
    }
    return out
  }, [listing])

  const entries = useMemo(() => {
    const list = listing?.entries ?? []
    const q = filter.trim().toLowerCase()
    return q ? list.filter((e) => e.name.toLowerCase().includes(q)) : list
  }, [listing, filter])

  const pending = Object.values(transfers)

  return (
    <div className="files">
      <div className="files-bar">
        <nav className="files-crumbs" aria-label="Path">
          {crumbs.map((c, i) => (
            <span key={c.path}>
              {i > 0 && <span className="files-sep">/</span>}
              <button className="files-crumb" onClick={() => void load(c.path)}>{c.label}</button>
            </span>
          ))}
          {listing && listing.home && listing.path !== listing.home && (
            <button className="btn btn-sm btn-ghost" onClick={() => void load(listing.home)} title="Home folder">~</button>
          )}
        </nav>
        <form
          className="files-goto"
          onSubmit={(e) => {
            e.preventDefault()
            void load(pathInput)
          }}
        >
          <input value={pathInput} onChange={(e) => setPathInput(e.target.value)} placeholder="/path/to/folder" spellCheck={false} />
        </form>
        <input className="files-filter" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" />
        <button className="btn btn-sm" onClick={() => void load(dir)} disabled={busy === 'list'}>↻</button>
        <button className="btn btn-sm btn-primary" onClick={() => void act('upload', () => api.files.upload(serverId, dir))} disabled={busy !== null} title="Choose files on this Mac and upload them here">
          ⬆ Upload
        </button>
        <button
          className="btn btn-sm"
          onClick={() => {
            void prompt('New folder name').then((name) => {
              if (name) void act('mkdir', () => api.files.mkdir(serverId, `${dir}/${name}`))
            })
          }}
          disabled={busy !== null}
        >
          + Folder
        </button>
        <button
          className="btn btn-sm"
          onClick={() => {
            void prompt('New file name').then((name) => {
              if (name) void act('touch', () => api.files.touch(serverId, `${dir}/${name}`))
            })
          }}
          disabled={busy !== null}
        >
          + File
        </button>
      </div>
      {error && <p className="error-text">{error}</p>}
      {pending.length > 0 && (
        <div className="files-transfers">
          {pending.map((t) => (
            <div key={t.name} className={`files-transfer${t.error ? ' is-bad' : ''}`}>
              <span className="files-transfer-name">{t.name}</span>
              {t.error ? <span className="error-text">{t.error}</span> : t.finished ? <span>done</span> : <progress value={t.done} max={t.total || 1} />}
              {!t.error && !t.finished && t.total > 0 && <span className="plugin-desc">{Math.round((t.done / t.total) * 100)}%</span>}
            </div>
          ))}
        </div>
      )}
      <div className={`files-split${open ? ' has-editor' : ''}`}>
        <div className="files-list">
          {listing && listing.path !== '/' && (
            <button className="files-row" onClick={() => void load(listing.parent)}>
              <span className="files-icon">↩</span>
              <span className="files-name">..</span>
            </button>
          )}
          {entries.map((e) => (
            <div key={e.path} className={`files-row${open?.path === e.path ? ' is-open' : ''}`}>
              <button className="files-main" onClick={() => void openFile(e)} title={e.path}>
                <span className="files-icon">{iconOf(e)}</span>
                <span className="files-name">
                  {e.name}
                  {e.link && <span className="plugin-desc"> →</span>}
                </span>
                <span className="files-size">{e.isDir ? '' : sizeOf(e.size)}</span>
                <span className="files-when">{when(e.modMs)}</span>
              </button>
              <span className="files-actions">
                {!e.isDir && (
                  <button className="icon-btn" title="Download" onClick={() => void act('download', () => api.files.download(serverId, e.path))}>
                    ⬇
                  </button>
                )}
                <button
                  className="icon-btn"
                  title="Rename"
                  onClick={() => {
                    void prompt('New name', e.name).then((name) => {
                      if (name && name !== e.name) void act('rename', () => api.files.rename(serverId, e.path, `${dir}/${name}`))
                    })
                  }}
                >
                  ✎
                </button>
                <button
                  className="icon-btn"
                  title="Delete"
                  onClick={() => {
                    void confirm(`Delete ${e.name}${e.isDir ? ' and everything in it' : ''}?`).then((ok) => {
                      if (ok) void act('delete', () => api.files.remove(serverId, e.path))
                    })
                  }}
                >
                  🗑
                </button>
              </span>
            </div>
          ))}
          {listing && entries.length === 0 && <p className="field-hint files-empty">{filter ? 'Nothing matches.' : 'Empty folder.'}</p>}
        </div>
        {open && (
          <div className="files-editor">
            <div className="files-editor-bar">
              <span className="files-editor-title" title={open.path}>
                {open.path.split('/').pop()}
                {dirty && <span className="files-dirty"> ●</span>}
              </span>
              <span className="plugin-desc">
                {sizeOf(open.size)} · {when(open.modMs)}
                {open.truncated ? ' · truncated at 2 MB, read-only' : ''}
              </span>
              {open.isText && !open.truncated && (
                <button className="btn btn-sm btn-primary" onClick={() => void save()} disabled={!dirty || busy === 'save'}>
                  {busy === 'save' ? 'Saving…' : 'Save'}
                </button>
              )}
              <button className="btn btn-sm" onClick={() => void act('download', () => api.files.download(serverId, open.path))}>
                Download
              </button>
              <button
                className="btn btn-sm btn-ghost"
                onClick={() => {
                  void (dirty ? confirm('Discard unsaved changes?') : Promise.resolve(true)).then((ok) => {
                    if (ok) setOpen(null)
                  })
                }}
              >
                Close
              </button>
            </div>
            {open.dataUrl ? (
              <div className="files-preview">
                <img src={open.dataUrl} alt={open.path} />
              </div>
            ) : open.isText ? (
              open.path.toLowerCase().endsWith('.md') && !dirty ? (
                <div className="files-md-wrap">
                  <div className="team-md files-md" dangerouslySetInnerHTML={{ __html: renderMarkdown(draft) }} />
                  <p className="field-hint">Preview. Click into the text below to edit.</p>
                  <textarea ref={editor} className="files-textarea" value={draft} readOnly={open.truncated} onChange={(e) => setDraft(e.target.value)} spellCheck={false} />
                </div>
              ) : (
                <textarea
                  ref={editor}
                  className="files-textarea"
                  value={draft}
                  readOnly={open.truncated}
                  spellCheck={false}
                  onChange={(e) => setDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if ((e.metaKey || e.ctrlKey) && e.key === 's') {
                      e.preventDefault()
                      void save()
                    }
                    if (e.key === 'Tab') {
                      e.preventDefault()
                      const el = e.currentTarget
                      const start = el.selectionStart
                      const next = `${draft.slice(0, start)}  ${draft.slice(el.selectionEnd)}`
                      setDraft(next)
                      requestAnimationFrame(() => el.setSelectionRange(start + 2, start + 2))
                    }
                  }}
                />
              )
            ) : (
              <div className="files-preview">
                <p className="field-hint">Binary file{open.mime ? ` (${open.mime})` : ''}. Download it to open it here.</p>
              </div>
            )}
          </div>
        )}
      </div>
      {ask && (
        <div className="lightbox files-ask" role="dialog" aria-label={ask.title} onClick={() => settle(null)}>
          <form
            className="files-ask-card"
            onClick={(e) => e.stopPropagation()}
            onSubmit={(e) => {
              e.preventDefault()
              settle(ask.kind === 'prompt' ? ask.value : '')
            }}
          >
            <h3>{ask.title}</h3>
            {ask.kind === 'prompt' && (
              <input autoFocus value={ask.value} onChange={(e) => setAsk({ ...ask, value: e.target.value })} spellCheck={false} onKeyDown={(e) => { if (e.key === 'Escape') settle(null) }} />
            )}
            <div className="btn-row">
              <button type="submit" className="btn btn-primary" autoFocus={ask.kind === 'confirm'}>
                {ask.kind === 'prompt' ? 'OK' : 'Yes'}
              </button>
              <button type="button" className="btn" onClick={() => settle(null)}>
                Cancel
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}
