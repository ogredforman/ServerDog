import { test } from 'node:test';
import assert from 'node:assert/strict';
import { copyWithFeedback, FEEDBACK_MS } from '../internal/web/static/js/logic/copy.js';

test('copyWithFeedback', async () => {
  const cases = [
    ['copy succeeds', async () => {}, 'copied'],
    ['copy rejects', async () => { throw new Error('denied'); }, 'failed'],
    ['writer throws synchronously', () => { throw new Error('no clipboard'); }, 'failed'],
  ];
  for (const [name, write, want] of cases) {
    const shown = [];
    const written = [];
    const timers = [];
    const cleared = [];
    const timer = await copyWithFeedback('ABC123', {
      write: (t) => { written.push(t); return write(t); },
      show: (s) => shown.push(s),
      setTimeout: (fn, ms) => { timers.push({ fn, ms }); return 7; },
      clearTimeout: (id) => cleared.push(id),
      timer: 3,
    });
    assert.deepEqual(written, ['ABC123'], name);
    assert.deepEqual(shown, [want], name);
    assert.deepEqual(cleared, [3], `${name}: earlier reset cleared`);
    assert.equal(timer, 7, name);
    assert.equal(timers.length, 1, name);
    assert.equal(timers[0].ms, FEEDBACK_MS, name);
    timers[0].fn();
    assert.deepEqual(shown, [want, 'idle'], `${name}: resets to idle`);
  }
});
