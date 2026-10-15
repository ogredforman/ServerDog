// Clock offset from the response Date header (SPEC §7.5 step 2). Pure.

// Offsets of this size or less are ignored: Date has whole-second resolution.
export const OFFSET_THRESHOLD_MS = 2000;

// clockOffset returns server time minus local time in milliseconds, to add to Date.now().
// It returns 0 when the header is missing or unparseable, or the difference is 2s or less.
export function clockOffset(dateHeader, localNow) {
  if (!dateHeader) return 0;
  const server = Date.parse(dateHeader);
  if (Number.isNaN(server)) return 0;
  const diff = server - localNow;
  return Math.abs(diff) > OFFSET_THRESHOLD_MS ? diff : 0;
}
