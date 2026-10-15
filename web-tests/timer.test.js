import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatDuration, formatTimer } from '../internal/web/static/js/logic/timer.js';

test('formatDuration', () => {
  const cases = [
    [0, '0:00'], [5, '0:05'], [65, '1:05'], [599, '9:59'], [3599, '59:59'],
    [3600, '1:00:00'], [3661, '1:01:01'], [36000, '10:00:00'], [59.9, '0:59'], [-12, '0:00'],
  ];
  for (const [sec, want] of cases) assert.equal(formatDuration(sec), want, `${sec}s`);
});

test('formatTimer by state', () => {
  const nowSec = 1_000_000;
  const now = nowSec * 1000;
  const cases = [
    [{ st: 'exact', ms: nowSec - 125 }, { text: '2:05', kind: 'exact' }],
    [{ st: 'exact', ms: nowSec - 3600 }, { text: '1:00:00', kind: 'exact' }],
    [{ st: 'estimate', ms: nowSec - 125 }, { text: '~2:05', kind: 'estimate' }],
    [{ st: 'starting', ms: nowSec - 10 }, { text: 'STARTING', kind: 'starting' }],
    [{ st: 'unknown', ms: null }, { text: '—', kind: 'unknown' }],
    [{ st: 'exact', ms: null }, { text: '—', kind: 'unknown' }],
    [{ st: 'exact', ms: nowSec + 30 }, { text: '0:00', kind: 'exact' }],
  ];
  for (const [server, want] of cases) assert.deepEqual(formatTimer(server, now), want, JSON.stringify(server));
});
