# Screenshot review notes (PLAN 2.5b)

What each reference screenshot shows and how the in-game UI behaves, recorded with the owner.
Colors are sampled from the PNGs; treat them as close, not exact. "ServerDog:" lines say what we
copy, change, or drop.

## 01-full-browser.png

Official server browser, reached by picking Official on the first screen. Region NA East was
preselected (last region played). The top row is hovered; nothing else is clicked.

- **Top bar** (`#131313`, thin divider below): logo, square back button (returns to the
  Official/Community picker), small grey `// SERVER BROWSER` above the title `OFFICIAL`.
  Account stats (cash, items) on the right.
  - ServerDog: drop the account stats; that space holds the ServerDog name and the disclaimer.
    The back button returns to the type picker.
- **Controls row:** outlined rectangular buttons (bg `#262728`, border `#555759`), small
  uppercase letter-spaced text: `REFRESH`, `SHOW FULL`, `SHOW EMPTY` on the left, `FILTER` on the right.
  - ServerDog: keep `SHOW FULL` / `SHOW EMPTY`. Drop `REFRESH` (data refreshes on its own) and
    `FILTER`. Find Fresh Starts goes in this row.
- **Bottom-left:** `JOIN BY ID` and `REGION [1/3]`. `[1/3]` = regions selected / max selectable.
  There are more than 3 regions, but you can filter by at most 3.
  - ServerDog: drop `JOIN BY ID`. Keep the region button with a plain `[n]` count (no max; see 03).
- **Table** (panel `#181919` on page `#1f2021`): columns are padlock (password indicator),
  shield-check (official), `SERVER NAME`, `MODE`, `MAP`, `PLAYERS`, `PING`.
  - Headers show `▶` when not sorted by that column, and an up/down arrow when sorted by it.
  - ServerDog: replace `PING` with the match timer column, in the same slot.
- **Rows** (`#3d3e3f`, ~8px dark gap between rows):
  - yellow shield-check on official servers;
  - name in uppercase, wide letter spacing, with ruleset tag chips under it (light grey chip,
    e.g. `PERSISTENT CASH / XP`);
  - mode, map and players centered;
  - ping shown as green bars (`#4fa340`) plus a number.
- **Hover:** lighter row (`#4c4d4e`) with a light grey border (`#9f9f9f`).
- **Right panel:** empty until a row is hovered. Then it shows the map image with `INCOMING…`,
  the big map name and coordinates, the server name (truncated with `…`), ruleset chips and
  `MAX ALLOWED PING 250 MS`.
  - ServerDog: no detail panel. Every row gets the gold copy-join-code button (see 04);
    a real Join Match link is parked.
- **Scrollbar:** thin, cream (`#eadfcc`).

## 02-type-picker.png (current) and 02-type-picker-old.jpg (preferred)

`02-type-picker.png` is the current in-game screen. The owner prefers the older two-card design
in `02-type-picker-old.jpg`, and ServerDog follows that.

- **Current:** title bar reads `DEPLOY`. Four cards, each with a photo, a white shield icon, a title
  and a grey `// subtitle`: large `OFFICIAL` and `COMMUNITY`, plus smaller `INFANTRY MODE // NON-COMBAT
  VEHICLES ONLY` and `LOW LEVEL // LEVEL CAPPED SERVERS` stacked on the right. Picking a card opens
  the server browser for that type. The browsers look about the same but are separate.
- **Infantry Mode / Low Level** are community servers. They also appear in the Community browser;
  the game added those cards only so players know they exist. In provider data they are
  `type: community` with a ruleset such as `infantry-only`.
- **Old (preferred):** two dark translucent square cards on a blurred background, each with a big
  white icon, a bold uppercase title and a short description in small caps. Community's description
  mentions "infantry only, hardcore & level capped servers". The hovered card gets a thin light border.
- ServerDog:
  - two cards, Official and Community, in the old layout;
  - our own simple icons (check-shield, group of people), no game logo, photos or icons
    (Bulkhead's artwork; avoids looking official);
  - infantry-only and low-level servers show up under Community with their ruleset chips;
  - the chosen type is saved in localStorage. A returning visitor goes straight to the saved
    browser, and the back button returns to the picker.

## 03-regionselector-noscroll.webp and 03-regionselector-scrolled-down.png

Region selection modal, opened from `REGION [n/3]`; first shot scrolled to the top, second to the bottom.

- **Modal:** near-black (`#0f0f0f`) with a thin gold-ish outline, over the darkened, blurred browser.
  - Title: big bold `SELECT REGION [1/3]`.
  - Under it: `AUTOMATICALLY PICK THE BEST REGION FOR ME` checkbox, "Or", and instructions
    (max 3, pick the closest).
- **Region list** (scrolls, white scrollbar): each row has a square checkbox, the uppercase display
  name and the ping (green bars + number).
  - Checkboxes are grey (`#9e9e9e`), or yellow (`#b8811a`) when checked.
  - Hovered row gets a thin grey outline.
  - The game allows at most 3.
- **Bottom:** small outlined `REFRESH`; gold gradient `CONFIRM` (dark brown to gold, gold border).
- **Display names, in game order, with provider IDs** (IDs from the live `/v1/snapshot`
  `meta.requestedRegions`, 2026-10-15):

  | Game name | Provider ID |
  |---|---|
  | Europe (Central) | `eu-central` |
  | Europe (East) | `eu-east` |
  | Europe (West) | `eu-west` |
  | Europe (South) | `eu-south` |
  | North America (Central) | `na-central` |
  | North America (North) | `na-north` |
  | North America (East) | `na-east` |
  | North America (West) | `na-west` |
  | South America | `south-america` |
  | Asia (East) | `asia-east` |
  | Asia (West) | `asia-west` |
  | Asia (South East) | `asia-se` |
  | Oceania | `oceania` |
  | South Africa | none (the provider doesn't poll it) |

- ServerDog:
  - Title `SELECT REGION [n]`, no max.
  - **Applies each click immediately**; no `CONFIRM`.
  - **No limit of 3:** any number of regions. The button shows the selected count without a max.
  - **Drop:** ping, the "automatically pick" checkbox and the modal's `REFRESH`.
  - **Region list:** regions in game order with game display names. An unknown ID falls back to
    the raw ID, listed after the known ones. South Africa is omitted until the provider has it.
  - **First visit** (nothing saved): preselect the region nearest the visitor if it can be
    worked out without a network request or permission prompt (e.g. from the browser's time
    zone). Otherwise select no region, which means all regions are shown.
  - The selection is saved in localStorage.

## 04-showfull-selected-sortedby-players.png and 04-showfull-selected-sortedby-playersasc.png

Toggle on/off states and the players sort in both directions. The second shot also has a row
selected.

- **Toggle on** (`SHOW FULL` in the first shot, `SHOW EMPTY` in the second): warm brown fill
  (`#523f21`), amber border (`#a66b11`), orange text (`#ff9500`).
  - Off: grey outlined button (fill `#333535` at this spot, border `#555759`, light grey text).
  - Each toggle switches independently.
- **Sorted header:** the sorted column's header cell gets a lighter background (`#3a3b3b` vs
  `#1e1e1e`) and its arrow turns up or down. Every other column keeps `▶`.
  - `▲` = descending (100/100 first); `▼` = ascending (5/100 first).
  - ServerDog follows this arrow convention.
- **Selected row** (click): gold gradient fill (about `#856519` to `#7c6325`), gold border
  (`#b79010`), text stays light. The selection is separate from hover; a hovered row keeps the grey
  border style at the same time.
- **Join Match:** when a row is selected, the right panel is replaced by a wide `JOIN MATCH`
  button: dark-to-gold gradient (`#211a0b` to `#4d3d14`), thin gold frame with `[ ▸ … ◂ ]` bracket
  corners, large white letter-spaced text.
  - ServerDog: a web page can't join a match (no known link scheme; parked). The same gold
    button style is used for "copy join code" on every row with a join code, in its own last
    column after Timer; selection stays separate (PLAN 2.7b).

## 06-rows-top-selected-second-hovered.png

Close-up of the rows at native size: the top row is selected and the second is hovered (styles as
in 01/04).

- **Row layout:**
  - narrow empty padlock cell, then a thin vertical divider before the yellow shield-check;
  - name on top: uppercase, wide letter spacing, light grey (`#cbcbcb`);
  - ruleset chip under the name: grey box (`#b9b9b9`-ish) with dark uppercase text;
  - thin vertical dividers between the mode, map, players and ping cells;
  - values centered, uppercase, letter-spaced.
- Each row has a subtle vertical gradient (lighter top edge) and an ~8px dark gap before the next.
- No password-protected, hardcore or community rows appear in any shot.
  - Assumption: the padlock column shows a padlock icon on password servers.
  - Assumption: other rulesets (`infantry-only`, `hardcore`) show as more chips, like
    `PERSISTENT CASH / XP` does for `standard`.

## 10-empty-state.png

What the game shows when nothing matches: `SHOW EMPTY` on, players sorted ascending, and an empty
table panel with headers only. No message.

- ServerDog: keep the empty table frame, but show a short message inside it saying which
  filters are active (type, regions, toggles). SPEC §11 requires this; the game has no equivalent.

## 11-fonts-columns.png and 11-fonts-header.png

Close-ups of the column headers and the title block.

- **Typeface:** a condensed-leaning grotesque that looks like **Barlow** (free, SIL OFL), at several
  weights:
  - title `OFFICIAL`: semi-bold, white;
  - `// SERVER BROWSER` and the headers: regular, grey, uppercase, letter-spaced;
  - row text: medium, uppercase, wide letter spacing.
- The game font files weren't supplied (`design/fonts/` is empty). ServerDog self-hosts Barlow as
  the closest free match; check it side by side in PLAN 2.6.
- **Header cells:** separate dark blocks (`#1e1e1e`) with small gaps between them. Padlock and
  shield icons are in grey. The sort arrow is a small solid triangle after the label.

## Not covered by the screenshots

- Map thumbnails: dropped (owner decision). The row has no map image in-game; the image only
  appeared in the right panel, which ServerDog drops, and the game's artwork isn't ours to copy.
- Password-protected, community, hardcore and infantry-only rows (see the assumptions under 06).
- Yellow accent for Find Fresh Starts: the game's gold, from the `CONFIRM` / `JOIN MATCH`
  gradient and the yellow checkbox (`#b8811a`), so it stands apart from the amber toggles.
