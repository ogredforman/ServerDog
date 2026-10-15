// Refresh scheduling for servers.json (SPEC §7.5 step 4). Time, fetch, timers and randomness are
// injected so tests run deterministically. `now()` is Unix milliseconds (offset-corrected by the
// caller); JSON times are Unix seconds.

// Retry delays when generatedAt hasn't changed or the fetch failed; the last one repeats.
export const BACKOFF_MS = [15000, 30000, 60000];

// Random delay added after nextUpdateAt so visitors don't all fetch at the same instant.
export const JITTER_MS = 10000;

// freshDelay is the wait after new data: until nextUpdateAt (never negative) plus 0–10s of jitter.
export function freshDelay(nextUpdateAt, now, random) {
  return Math.max(0, nextUpdateAt * 1000 - now) + random() * JITTER_MS;
}

// backoffDelay is the wait before retry number `attempt` (0-based), capped at the last step.
export function backoffDelay(attempt) {
  return BACKOFF_MS[Math.min(attempt, BACKOFF_MS.length - 1)];
}

// createRefresher fetches `url` now and then on the schedule above, calling onData(data, dateHeader)
// only for new data (a changed generatedAt). Failures call onError(err) and keep the current data.
export function createRefresher({ url, fetch, now, setTimeout, clearTimeout, random, onData, onError = () => {} }) {
  let current = null;
  let attempt = 0;
  let timer = null;
  let running = false;
  let generation = 0; // bumped by stop(), so a fetch from before a restart is ignored

  function schedule(delay) {
    timer = setTimeout(tick, delay);
  }

  function backoff() {
    schedule(backoffDelay(attempt));
    attempt++;
  }

  async function load() {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`fetch ${url}: HTTP ${res.status}`);
    const data = await res.json();
    if (!Number.isFinite(data?.generatedAt) || !Number.isFinite(data?.nextUpdateAt)) {
      throw new Error(`fetch ${url}: missing generatedAt or nextUpdateAt`);
    }
    return { data, dateHeader: res.headers?.get?.('Date') ?? null };
  }

  async function tick() {
    timer = null;
    const gen = generation;
    const live = () => running && gen === generation;
    let result;
    try {
      result = await load();
    } catch (err) {
      if (!live()) return;
      onError(err);
      backoff();
      return;
    }
    if (!live()) return;
    if (current && result.data.generatedAt === current.generatedAt) {
      backoff();
      return;
    }
    current = result.data;
    attempt = 0;
    schedule(freshDelay(current.nextUpdateAt, now(), random));
    onData(current, result.dateHeader);
  }

  return {
    start() {
      if (running) return;
      running = true;
      tick();
    },
    stop() {
      running = false;
      generation++;
      if (timer !== null) clearTimeout(timer);
      timer = null;
    },
    data() {
      return current;
    },
  };
}
