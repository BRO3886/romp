# Configuration: TOML layered with a zero-means-default overlay

Status: accepted

## Context

romp's settings live in up to four places — built-in defaults, `~/.config/romp/config.toml`, the committed `romp.toml`, and the gitignored `.romp/local.toml` — with command flags on top. Layering four files over defaults needs a merge rule, and the obvious libraries (koanf, viper) exist precisely for this. The open questions are whether that machinery is worth it, and what "not set in this file" means for a single field.

## Decision

Config is TOML (the format the README already promises), parsed with `github.com/BurntSushi/toml`. Files merge in a fixed precedence — defaults → user config → `romp.toml` → `.romp/local.toml` → flags — by unmarshalling each file into a fresh struct and copying only non-zero fields over the accumulated result. Zero value means "use the lower layer": `width = 0`, `base = ""`, an unset `max_turns`, and empty strings all fall through, so a file overrides exactly what it writes and nothing else. The user config path follows `$XDG_CONFIG_HOME` with a `~/.config` fallback (matching the README), not `os.UserConfigDir`, which on macOS points at `~/Library/Application Support` and ignores XDG.

`history_days` is the one field exempt from the additive rule: it is copied only from the user config file, and values written in `romp.toml` or `.romp/local.toml` are ignored. Retention is an operator concern, not a team convention — a repo should not dictate how long another machine keeps its history — so the carve-out is intentional (see ADR 0008).

After the merge, `Load` validates `[harness]`: `default` must be `claude`, `codex`, or `opencode`. Claude's live set is `low | medium | high | xhigh | max`; Codex adds `none | minimal | ultra`. OpenCode passes `effort` through as the model-specific `--variant` value and does not validate it. Shared names pass through unchanged. The check runs after flags, so `romp run --harness claude` with `effort = "ultra"` fails at load, while an OpenCode variant remains the model's responsibility.

## Consequences

- Layering is additive per field: `romp.toml` can set `[verify]` while `.romp/local.toml` sets only `[harness]` model, without the local file clobbering the committed verify contract.
- The `history_days` carve-out is the only asymmetry. It exists because the default layering would otherwise let a committed file (shared with the team) change machine-local behavior; keeping one field global-only is cheaper than adding a separate global config file.
- A field can never be explicitly set to its zero value — `width = 0` reads as "unset, use default". That is exactly romp's intended semantics for every configurable field today, so nothing is lost; a future field whose meaningful value includes zero would need a pointer or sentinel instead.
- koanf and viper were rejected: romp reads flat files once at startup and never needs env expansion, watching, or a multi-source provider registry, so the extra surface buys only indirection.
- `romp init` writes the user-selected `[verify].commands` list; runtime discovery stays rejected (see ADR 0011).
