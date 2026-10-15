// Stale-data detection (SPEC §7.5 step 5). Pure: `now` is Unix milliseconds.

// isStale is true when the server says so, or when generatedAt (Unix seconds) is older than
// 2 × refreshSeconds + 60s. Missing or malformed times count as stale (SPEC §5 Honest data).
export function isStale(data, now) {
  if (data.stale) return true;
  if (!Number.isFinite(data.generatedAt) || !Number.isFinite(data.refreshSeconds)) return true;
  return now / 1000 - data.generatedAt > 2 * data.refreshSeconds + 60;
}
