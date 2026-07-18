interface Props {
  size?: number;
  className?: string;
}

// Viaduct mark: a three-arch aqueduct bridge — deck resting on evenly spaced
// arches. Renders in currentColor so it inherits whatever foreground the
// caller sets (e.g. primary-foreground on a colored badge).
export function Logo({ size = 16, className }: Props) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      className={className}
      aria-hidden
    >
      <path d="M2 5H22" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path
        d="M2 19V8a3 3 0 0 1 6 0v11M9 19V8a3 3 0 0 1 6 0v11M16 19V8a3 3 0 0 1 6 0v11"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path d="M1 19.5H23" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}
