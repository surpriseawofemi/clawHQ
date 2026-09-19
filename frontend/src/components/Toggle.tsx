/** A switch: label on the left, pill on the right. */
export function Toggle({ on, onChange, label, title }: { on: boolean; onChange: (v: boolean) => void; label: string; title?: string }): React.JSX.Element {
  return (
    <button type="button" className={`toggle${on ? ' is-on' : ''}`} role="switch" aria-checked={on} onClick={() => onChange(!on)} title={title}>
      <span className="toggle-label">{label}</span>
      <span className="toggle-pill" aria-hidden="true">
        <span className="toggle-knob" />
      </span>
    </button>
  )
}
