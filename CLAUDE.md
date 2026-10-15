@AGENTS.md

## Claude Code

- Use plan mode for any task that touches the `source.Source` / `clock.Clock` interfaces (SPEC §10.3), the tracker's tier rules (SPEC §7.2–7.3), or the published `servers.json` shape (SPEC §8).
- Orient with `docs/CODEMAP.md` before reading code.
- Workflows (devflow plugin): `/devflow:next-task` (do the next PLAN task), `/devflow:park <idea>`
  (capture an idea without doing it), `/devflow:progress` (where things stand).
- The `devflow:reviewer` subagent checks diffs against the hard rules above. Use it before finishing any task.
- Hooks (`tools/hooks`, wired in `.claude/settings.json`):
  - `gofmt` runs after edits;
  - you're asked before adding dependencies, and `CGO_ENABLED=1` is refused;
  - `go build` + `go vet` must pass when you stop.

  If a hook blocks you, fix the cause; don't work around it.
