import { Logo } from './Logo'

type Props = {
  /** Page name shown beside the back button, e.g. "Activity". */
  title: string
  onBack: () => void
}

/**
 * The same top strip on every page: the brand row (which also makes room for the
 * macOS traffic lights and drags the window), then "‹ Menu" with the page name.
 * Chat's sidebar has the identical pair, so pages look alike wherever you are.
 */
export function PageTop({ title, onBack }: Props): React.JSX.Element {
  return (
    <>
      <header className="sidebar-head">
        <span className="brand">
          <Logo size={22} className="brand-mark" />
          <span className="brand-name">ClawHQ</span>
        </span>
      </header>
      <div className="page-back-bar">
        <button className="back-btn" onClick={onBack} title="Back to the main menu">
          ‹ Menu
        </button>
        <span className="plugin-desc">{title}</span>
      </div>
    </>
  )
}
