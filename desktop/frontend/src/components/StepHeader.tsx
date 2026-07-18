interface Props {
  eyebrow?: string;
  title: string;
  subtitle?: string;
}

export function StepHeader({ eyebrow, title, subtitle }: Props) {
  return (
    <div className="mb-4">
      {eyebrow && (
        <div className="mb-0.5 text-xs font-semibold uppercase tracking-[0.14em] text-accent-strong">
          {eyebrow}
        </div>
      )}
      <h2 className="text-base font-semibold tracking-tight text-ink">{title}</h2>
      {subtitle && <p className="mt-1 text-sm leading-relaxed text-dim">{subtitle}</p>}
    </div>
  );
}
