import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isStale } from '../internal/web/static/js/logic/stale.js';

test('isStale', () => {
  const base = { generatedAt: 1000, refreshSeconds: 30, stale: false };
  // Threshold: 2 × 30 + 60 = 120s after generatedAt.
  const cases = [
    ['fresh', base, 1010, false],
    ['exactly at threshold', base, 1120, false],
    ['just past threshold', base, 1120.001, true],
    ['server flag wins', { ...base, stale: true }, 1000, true],
    ['missing generatedAt', { refreshSeconds: 30, stale: false }, 1000, true],
    ['missing refreshSeconds', { generatedAt: 1000, stale: false }, 1000, true],
  ];
  for (const [name, data, nowSec, want] of cases) assert.equal(isStale(data, nowSec * 1000), want, name);
});
