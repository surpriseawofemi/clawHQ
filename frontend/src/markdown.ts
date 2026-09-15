import DOMPurify from 'dompurify'
import { marked } from 'marked'

marked.setOptions({ gfm: true, breaks: false })

/**
 * Markdown to HTML, sanitised. Agent documents are written by models and may quote
 * anything, so nothing script-like survives, and links open outside the app.
 */
export function renderMarkdown(source: string): string {
  const html = marked.parse(source, { async: false }) as string
  return DOMPurify.sanitize(html, {
    USE_PROFILES: { html: true },
    ADD_ATTR: ['target', 'rel']
  }).replace(/<a /g, '<a target="_blank" rel="noopener noreferrer" ')
}
