import { Component, type ErrorInfo, type ReactNode } from 'react'
import { api } from '../api'

type State = { error: Error | null; info: string; logPath: string }

/**
 * A render or effect error in React 18 unmounts the whole tree: a blank window with
 * no clue. This catches it, shows the message and stack, writes both to the
 * frontend log on disk, and offers a reload.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null, info: '', logPath: '' }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    const stack = `${error.stack ?? ''}\n--- component stack ---${info.componentStack ?? ''}`
    this.setState({ info: info.componentStack ?? '' })
    void api.diag.report('render', `${error.name}: ${error.message}`, stack).catch(() => undefined)
    void api.diag
      .path()
      .then((p) => this.setState({ logPath: p }))
      .catch(() => undefined)
  }

  render(): ReactNode {
    const { error, info, logPath } = this.state
    if (!error) return this.props.children
    return (
      <div className="crash">
        <h2>ClawHQ hit an error</h2>
        <p className="crash-msg">
          {error.name}: {error.message}
        </p>
        <pre className="crash-stack">
          {error.stack}
          {info && `\n--- component stack ---${info}`}
        </pre>
        {logPath && <p className="field-hint">Written to {logPath}</p>}
        <div className="btn-row">
          <button className="btn btn-primary" onClick={() => window.location.reload()}>
            Reload
          </button>
          <button
            className="btn"
            onClick={() => void navigator.clipboard.writeText(`${error.name}: ${error.message}\n${error.stack ?? ''}\n${info}`)}
          >
            Copy details
          </button>
        </div>
      </div>
    )
  }
}
