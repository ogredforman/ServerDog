# In-game UI reference screenshots

ServerDog's UI is modeled on the Wardogs in-game server browser (SPEC §11). Put reference
screenshots here. PNG is best, at native resolution, with no scaling or compression.

Name files with a number prefix and a short description, e.g. `01-browser-full.png`,
`04-row-hover.png`. Use `NN-name.png` for each shot.

## Shots to capture

1. **Full server browser:** the whole screen with a typical populated list.
2. **Server type picker:** however you choose official vs community on entry, plus the selected state.
3. **Region selector:** closed and open, showing every region's display name.
4. **Controls:** Show Full / Show Empty toggles in both on and off states, plus any search or refresh controls.
5. **Column headers:** the default state, plus a column sorted ascending and descending if the sort indicator differs.
6. **Rows close-up:** a few rows at 100% zoom, including a full server, an empty one, a password-protected one, and a hardcore / infantry-only one if they look different.
7. **Row states:** hover and selected.
8. **Map thumbnails:** not used. ServerDog shows map names only and uses no game artwork.
9. **Yellow accent:** anything using the game's yellow accent (buttons, highlights), so the Find Fresh Starts button can match it.
10. **Empty state / no results:** what the game shows when filters hide everything.
11. **Fonts:** a clear close-up of headings and body text (the game font files aren't available; ServerDog self-hosts Barlow).

Add a line below for any shot that needs explaining (e.g. "the region dropdown scrolls, more regions hidden").

## Notes

The owner's review of every shot is in [`NOTES.md`](NOTES.md).
