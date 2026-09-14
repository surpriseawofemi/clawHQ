type Props = {
  /** Rendered size in px. */
  size?: number
  className?: string
}

/**
 * The ClawHQ mark: an open pincer, carried over from the app's OpenClaw origins.
 *
 * Drawn in `currentColor` and with no tile behind it, so the same glyph works on the
 * dark and light sidebars and inherits whatever colour its container sets.
 */
export function Logo({ size = 20, className }: Props): React.JSX.Element {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 1024 1024"
      className={className}
      role="img"
      aria-label="ClawHQ"
      fill="none"
    >
      <g
        fill="currentColor"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        transform="translate(-6 34)"
      >
        <ellipse cx="352" cy="556" rx="168" ry="152" transform="rotate(-20 352 556)" />
        <path
          d="M 372 470 C 512 374 656 300 768 294 C 812 292 826 348 798 388"
          fill="none"
          strokeWidth="116"
        />
        <path d="M 392 648 C 528 712 668 696 764 606" fill="none" strokeWidth="104" />
      </g>
    </svg>
  )
}
