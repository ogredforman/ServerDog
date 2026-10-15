// Type, region and Show Full / Show Empty filters (SPEC §8 Browser UI). Pure.

// isFull is true when players reach max, which includes 0/0 servers.
export const isFull = (s) => s.p >= s.mp;

// isEmpty is true when nobody is playing.
export const isEmpty = (s) => s.p === 0;

// filterServers returns the servers matching the chosen type ('o' / 'c'), regions (an empty
// or missing list means all regions) and toggles. A 0/0 server is both full and empty, so
// turning off either toggle hides it. The input array is not modified.
export function filterServers(servers, { type, regions = [], showFull = true, showEmpty = true } = {}) {
  const regionSet = new Set(regions);
  return servers.filter((s) =>
    (!type || s.t === type) &&
    (regionSet.size === 0 || regionSet.has(s.r)) &&
    (showFull || !isFull(s)) &&
    (showEmpty || !isEmpty(s)));
}
