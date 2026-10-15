import { test } from 'node:test';
import assert from 'node:assert/strict';
import { backoffDelay, createRefresher, freshDelay } from '../internal/web/static/js/logic/refresh.js';

test('freshDelay', () => {
  const cases = [
    ['waits until nextUpdateAt', 1180, 1000_000, () => 0, 180_000],
    ['adds jitter', 1180, 1000_000, () => 0.5, 185_000],
    ['jitter below 10s', 1180, 1000_000, () => 0.999, 189_990],
    ['nextUpdateAt passed: jitter only', 990, 1000_000, () => 0.25, 2_500],
  ];
  for (const [name, next, now, random, want] of cases) assert.equal(freshDelay(next, now, random), want, name);
});

test('backoffDelay', () => {
  const cases = [[0, 15000], [1, 30000], [2, 60000], [3, 60000], [10, 60000]];
  for (const [attempt, want] of cases) assert.equal(backoffDelay(attempt), want, `attempt ${attempt}`);
});

const ok = (data, date = 'Thu, 15 Oct 2026 12:00:00 GMT') => ({
  ok: true,
  status: 200,
  headers: { get: (h) => (h === 'Date' ? date : null) },
  json: async () => data,
});

// harness runs a refresher against scripted responses. Each script entry is a response object,
// an Error (fetch rejects), or a function returning either.
function harness(script, { nowMs = 1000_000, random = () => 0 } = {}) {
  const h = { delays: [], loaded: [], errors: [], fetches: 0, pending: null, cleared: 0 };
  const r = createRefresher({
    url: '/data/servers.json',
    fetch: async (url) => {
      assert.equal(url, '/data/servers.json');
      const step = script[h.fetches++];
      if (step instanceof Error) throw step;
      return step;
    },
    now: () => nowMs,
    random,
    setTimeout: (fn, ms) => {
      h.delays.push(ms);
      h.pending = fn;
      return h.delays.length;
    },
    clearTimeout: () => {
      h.cleared++;
      h.pending = null;
    },
    onData: (data, date) => h.loaded.push([data.generatedAt, date]),
    onError: (err) => h.errors.push(err.message),
  });
  h.r = r;
  // flush lets the async fetch/json chain settle.
  h.flush = () => new Promise((resolve) => setImmediate(resolve));
  h.fire = async () => {
    const fn = h.pending;
    h.pending = null;
    fn();
    await h.flush();
  };
  return h;
}

const data = (generatedAt) => ({ generatedAt, nextUpdateAt: generatedAt + 180, servers: [] });

test('first load then schedule at nextUpdateAt plus jitter', async () => {
  const h = harness([ok(data(1000))], { random: () => 0.5 });
  h.r.start();
  await h.flush();
  assert.deepEqual(h.loaded, [[1000, 'Thu, 15 Oct 2026 12:00:00 GMT']]);
  assert.deepEqual(h.delays, [180_000 + 5_000]);
  assert.equal(h.r.data().generatedAt, 1000);
});

test('unchanged generatedAt backs off 15s, 30s, 60s, then stays at 60s', async () => {
  const h = harness([ok(data(1000)), ok(data(1000)), ok(data(1000)), ok(data(1000)), ok(data(1000))]);
  h.r.start();
  await h.flush();
  for (let i = 0; i < 4; i++) await h.fire();
  assert.deepEqual(h.delays, [180_000, 15000, 30000, 60000, 60000]);
  assert.equal(h.loaded.length, 1);
});

test('new data resets the backoff', async () => {
  const h = harness([ok(data(1000)), ok(data(1000)), ok(data(1000)), ok(data(1200)), ok(data(1200))], {
    nowMs: 1200_000,
  });
  h.r.start();
  await h.flush();
  for (let i = 0; i < 4; i++) await h.fire();
  // 1000+180 is in the past at now=1200s, so the first wait is jitter only (0 here).
  assert.deepEqual(h.delays, [0, 15000, 30000, 180_000, 15000]);
  assert.deepEqual(h.loaded.map(([g]) => g), [1000, 1200]);
});

test('failures keep the current data and share the backoff', async () => {
  const cases = [
    ['fetch rejects', new Error('network down')],
    ['HTTP 500', { ok: false, status: 500, headers: { get: () => null }, json: async () => ({}) }],
    ['bad JSON', { ok: true, status: 200, headers: { get: () => null }, json: async () => { throw new SyntaxError('bad'); } }],
    ['missing times', ok({ servers: [] })],
  ];
  for (const [name, failure] of cases) {
    const h = harness([ok(data(1000)), ok(data(1000)), failure, failure]);
    h.r.start();
    await h.flush();
    for (let i = 0; i < 3; i++) await h.fire();
    assert.deepEqual(h.delays, [180_000, 15000, 30000, 60000], name);
    assert.equal(h.loaded.length, 1, name);
    assert.equal(h.errors.length, 2, name);
    assert.equal(h.r.data().generatedAt, 1000, name);
  }
});

test('failure before any data backs off without onData', async () => {
  const h = harness([new Error('offline'), ok(data(1000))]);
  h.r.start();
  await h.flush();
  assert.deepEqual(h.delays, [15000]);
  assert.equal(h.r.data(), null);
  await h.fire();
  assert.deepEqual(h.loaded.map(([g]) => g), [1000]);
  assert.deepEqual(h.delays, [15000, 180_000]);
});

test('stop cancels the pending fetch and ignores in-flight results', async () => {
  const h = harness([ok(data(1000)), ok(data(1200))]);
  h.r.start();
  await h.flush();
  h.r.stop();
  assert.equal(h.cleared, 1);
  assert.equal(h.pending, null);

  const g = harness([ok(data(1000))]);
  g.r.start();
  g.r.stop();
  await g.flush();
  assert.deepEqual(g.loaded, []);
  assert.deepEqual(g.delays, []);
});

test('restart while a fetch is in flight keeps one fetch chain', async () => {
  const h = harness([ok(data(1000)), ok(data(1200))]);
  h.r.start();
  h.r.stop();
  h.r.start();
  await h.flush();
  assert.deepEqual(h.loaded.map(([g]) => g), [1200]);
  assert.equal(h.delays.length, 1);
});
