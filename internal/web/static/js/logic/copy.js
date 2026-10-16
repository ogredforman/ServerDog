// Copy-join-code feedback (SPEC §8 Browser UI). Pure: the clipboard writer and timers are passed in.

// COPY_LABELS are the button labels for each feedback state.
export const COPY_LABELS = { idle: 'Copy join code', copied: 'Copied', failed: 'Copy failed' };

// FEEDBACK_MS is how long "Copied" / "Copy failed" stays before the label returns to idle.
export const FEEDBACK_MS = 1500;

// copyWithFeedback writes text, calls show('copied') or show('failed'), clears any earlier reset
// timer and schedules show('idle') after FEEDBACK_MS. It resolves to the new reset timer.
export async function copyWithFeedback(text, { write, show, setTimeout, clearTimeout, timer }) {
  let ok = true;
  try {
    await write(text);
  } catch {
    ok = false;
  }
  show(ok ? 'copied' : 'failed');
  clearTimeout(timer);
  return setTimeout(() => show('idle'), FEEDBACK_MS);
}
