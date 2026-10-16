// Region display names in game order, and the first-visit region guess (SPEC §11 Region
// selector, design/screenshots/NOTES.md 03). Pure: callers pass the time zone.

// REGIONS lists [provider ID, in-game display name] in the game's order. The provider doesn't
// poll South Africa yet; its ID is assumed.
export const REGIONS = Object.freeze([
  ['eu-central', 'Europe (Central)'],
  ['eu-east', 'Europe (East)'],
  ['eu-west', 'Europe (West)'],
  ['eu-south', 'Europe (South)'],
  ['na-central', 'North America (Central)'],
  ['na-north', 'North America (North)'],
  ['na-east', 'North America (East)'],
  ['na-west', 'North America (West)'],
  ['south-america', 'South America'],
  ['asia-east', 'Asia (East)'],
  ['asia-west', 'Asia (West)'],
  ['asia-se', 'Asia (South East)'],
  ['oceania', 'Oceania'],
  ['south-africa', 'South Africa'],
]);

const names = new Map(REGIONS);
const order = new Map(REGIONS.map(([id], i) => [id, i]));

// regionName is the display name for a region ID, or the ID itself when it isn't known.
export function regionName(id) {
  return names.get(id) ?? id;
}

// regionList orders region IDs for the selector: known IDs in game order, then unknown IDs
// alphabetically. Each entry is {id, name}.
export function regionList(ids) {
  const known = [];
  const unknown = [];
  for (const id of new Set(ids)) (order.has(id) ? known : unknown).push(id);
  known.sort((a, b) => order.get(a) - order.get(b));
  unknown.sort();
  return [...known, ...unknown].map((id) => ({ id, name: regionName(id) }));
}

// selectorRegions is what the region selector lists: every known region in game order, whether
// or not it has servers right now, then unknown IDs from the data alphabetically.
export function selectorRegions(dataIds) {
  return regionList([...REGIONS.map(([id]) => id), ...dataIds]);
}

// chosenRegions keeps the saved region IDs that still mean something: known regions (even with
// no servers right now) and IDs in the data. Others are ignored, so a region that has gone away
// doesn't hide everything.
export function chosenRegions(saved, dataIds) {
  const present = new Set(dataIds);
  return saved.filter((id) => order.has(id) || present.has(id));
}

// Exact IANA zones checked before the prefix rules below.
const zones = {
  'Europe/London': 'eu-west', 'Europe/Dublin': 'eu-west', 'Europe/Lisbon': 'eu-west',
  'Europe/Paris': 'eu-west', 'Europe/Brussels': 'eu-west', 'Europe/Amsterdam': 'eu-west',
  'Europe/Luxembourg': 'eu-west', 'Atlantic/Reykjavik': 'eu-west', 'Atlantic/Canary': 'eu-west',
  'Europe/Berlin': 'eu-central', 'Europe/Zurich': 'eu-central', 'Europe/Vienna': 'eu-central',
  'Europe/Prague': 'eu-central', 'Europe/Copenhagen': 'eu-central', 'Europe/Oslo': 'eu-central',
  'Europe/Stockholm': 'eu-central', 'Europe/Budapest': 'eu-central', 'Europe/Bratislava': 'eu-central',
  'Europe/Ljubljana': 'eu-central', 'Europe/Zagreb': 'eu-central',
  'Europe/Madrid': 'eu-south', 'Europe/Rome': 'eu-south', 'Europe/Athens': 'eu-south',
  'Europe/Malta': 'eu-south', 'Europe/Belgrade': 'eu-south', 'Europe/Sarajevo': 'eu-south',
  'Europe/Skopje': 'eu-south', 'Europe/Podgorica': 'eu-south', 'Europe/Tirane': 'eu-south',
  'Europe/Sofia': 'eu-south', 'Europe/Istanbul': 'eu-south', 'Asia/Nicosia': 'eu-south',
  // North America by longitude; Canada included. Nothing guesses na-north.
  'America/New_York': 'na-east', 'America/Detroit': 'na-east', 'America/Toronto': 'na-east',
  'America/Montreal': 'na-east', 'America/Halifax': 'na-east', 'America/St_Johns': 'na-east',
  'America/Moncton': 'na-east', 'America/Indiana/Indianapolis': 'na-east',
  'America/Kentucky/Louisville': 'na-east', 'America/Puerto_Rico': 'na-east',
  'America/Chicago': 'na-central', 'America/Winnipeg': 'na-central', 'America/Regina': 'na-central',
  'America/Mexico_City': 'na-central', 'America/Monterrey': 'na-central', 'America/Denver': 'na-central',
  'America/Edmonton': 'na-central', 'America/Boise': 'na-central',
  'America/Phoenix': 'na-west', 'America/Los_Angeles': 'na-west', 'America/Vancouver': 'na-west',
  'America/Tijuana': 'na-west', 'America/Anchorage': 'na-west', 'Pacific/Honolulu': 'na-west',
  'Asia/Tokyo': 'asia-east', 'Asia/Seoul': 'asia-east', 'Asia/Shanghai': 'asia-east',
  'Asia/Hong_Kong': 'asia-east', 'Asia/Taipei': 'asia-east', 'Asia/Macau': 'asia-east',
  'Asia/Singapore': 'asia-se', 'Asia/Kuala_Lumpur': 'asia-se', 'Asia/Jakarta': 'asia-se',
  'Asia/Bangkok': 'asia-se', 'Asia/Manila': 'asia-se', 'Asia/Ho_Chi_Minh': 'asia-se',
  'Asia/Saigon': 'asia-se', 'Asia/Phnom_Penh': 'asia-se', 'Asia/Yangon': 'asia-se',
  'Asia/Kolkata': 'asia-west', 'Asia/Calcutta': 'asia-west', 'Asia/Dubai': 'asia-west',
  'Asia/Karachi': 'asia-west', 'Asia/Riyadh': 'asia-west', 'Asia/Qatar': 'asia-west',
  'Asia/Kuwait': 'asia-west', 'Asia/Bahrain': 'asia-west', 'Asia/Muscat': 'asia-west',
  'Asia/Tehran': 'asia-west', 'Asia/Jerusalem': 'asia-west', 'Asia/Dhaka': 'asia-west',
  'Asia/Colombo': 'asia-west', 'Asia/Kathmandu': 'asia-west',
};

// Prefix rules for zones not listed above.
const prefixes = [
  ['Australia/', 'oceania'],
  ['Pacific/Auckland', 'oceania'],
  ['Pacific/Fiji', 'oceania'],
  ['America/Argentina/', 'south-america'],
  ['America/Sao_Paulo', 'south-america'],
  ['America/Santiago', 'south-america'],
  ['America/Bogota', 'south-america'],
  ['America/Lima', 'south-america'],
  ['America/Caracas', 'south-america'],
  ['America/Montevideo', 'south-america'],
  ['America/Asuncion', 'south-america'],
  ['America/La_Paz', 'south-america'],
  ['America/Guayaquil', 'south-america'],
  ['America/Indiana/', 'na-east'],
  ['America/Kentucky/', 'na-east'],
  ['America/North_Dakota/', 'na-central'],
  ['Europe/', 'eu-east'], // the remaining European zones are eastern (Warsaw, Kyiv, Helsinki, …)
];

// guessRegion maps an IANA time zone (from Intl, no request or permission) to the nearest
// region ID, or null when it can't tell.
export function guessRegion(timeZone) {
  if (typeof timeZone !== 'string') return null;
  if (zones[timeZone]) return zones[timeZone];
  for (const [prefix, id] of prefixes) {
    if (timeZone.startsWith(prefix)) return id;
  }
  return null;
}
