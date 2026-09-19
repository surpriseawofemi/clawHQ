/** Small line icons in currentColor, so they scale with text and match both themes. */
const base = { width: 14, height: 14, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 2, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true }

export const TrashIcon = (): React.JSX.Element => (
  <svg {...base}>
    <path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3" />
  </svg>
)
export const EditIcon = (): React.JSX.Element => (
  <svg {...base}>
    <path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" />
  </svg>
)
export const PowerIcon = (): React.JSX.Element => (
  <svg {...base}>
    <path d="M12 3v9M18.4 6.6a9 9 0 1 1-12.8 0" />
  </svg>
)
export const RefreshIcon = (): React.JSX.Element => (
  <svg {...base}>
    <path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6" />
  </svg>
)
export const DownloadIcon = (): React.JSX.Element => (
  <svg {...base}>
    <path d="M12 4v12M6 10l6 6 6-6M4 20h16" />
  </svg>
)
