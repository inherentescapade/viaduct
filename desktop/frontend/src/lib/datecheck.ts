const rel = /^\d+[dhm]$/;
const ymd = /^\d{4}-\d{2}-\d{2}$/;

// dateValid mirrors dates.Parse on the Go side closely enough for inline UI
// feedback. An empty string is valid (it just means "no filter").
export function dateValid(s: string): boolean {
  const t = s.trim();
  if (!t) return true;
  if (rel.test(t) || ymd.test(t)) return true;
  return !Number.isNaN(Date.parse(t));
}
