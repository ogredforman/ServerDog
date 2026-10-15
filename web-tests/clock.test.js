import { test } from 'node:test';
import assert from 'node:assert/strict';
import { clockOffset } from '../internal/web/static/js/logic/clock.js';

test('clockOffset', () => {
  const header = 'Thu, 15 Oct 2026 12:00:00 GMT';
  const server = Date.parse(header);
  const cases = [
    ['in sync', header, server, 0],
    ['exactly 2s behind ignored', header, server - 2000, 0],
    ['exactly 2s ahead ignored', header, server + 2000, 0],
    ['just over 2s behind applied', header, server - 2001, 2001],
    ['local ahead applied', header, server + 90_000, -90_000],
    ['missing header', null, server, 0],
    ['garbage header', 'not a date', server, 0],
  ];
  for (const [name, h, local, want] of cases) assert.equal(clockOffset(h, local), want, name);
});
