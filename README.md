# v - use vim motions and natively manage your socks

`v` is a Neovim session manager and lightweight multiplexer replacement. It
runs as the foreground process of a single terminal window: a picker of
Neovim sessions that attaches the chosen server with `nvim --server SOCK
--remote-ui` and returns to the picker whenever the client exits. Servers run
headless and detached, so closing the terminal loses nothing.

## Install

Installs v into $PREFIX/bin (default /usr/local)

```bash
make install
```

Requirements at runtime: `nvim` and `fzf` on `PATH`. `stty` is used to save
and restore the terminal.

## Neovim mappings it relies on

The hub assumes the user's `init.lua` maps `<leader>d` and terminal-mode
`<C-\><C-d>` to `:detach`. When the client exits (detach, or the server
quit), the hub redisplays the picker.

## Commands

| Command | Effect |
| --- | --- |
| `v` | hub, all rows (live + dormant repos) |
| `v -j`, `v --jump` | same as `v` |
| `v -a`, `v --attach` | hub, live rows only |
| `v -l`, `v --list` | table (`--json` for a JSON array of sessions) |
| `v -k`, `v --kill` | pick (multi-select) and force-quit sessions |
| `v -n NAME` | new session `project` or `project.role` |
| `v -p P` / `v -r R` | project / role for a new session |
| `v --load PATTERN [DIR]` | new session with up to 40 matching files |
| `v [DIR]` | new session started in `DIR` |
| `v [FILES...]` | new session with files (or, inside nvim, open them here) |
| `v -t X` | inside nvim: send files to session `X` |
| `v --mv NAME TARGET` | move a session to project or project.role |
| `v --detached` | create without attaching, print the socket path |
| `v --startuptime` | pass `--startuptime` to the new server |
| `v -h`, `v --help` | usage |

State lives under `${XDG_STATE_HOME:-$HOME/.local/state}/v`: `sockets/`,
`logs/`, `mru`.

## Decisions

- **Sanitize does not append `_`.** The bash original used a here-string that
  turned a trailing newline into a trailing `_`. Go's `proj.Sanitize("main")`
  returns `main`. Existing sockets ending in `_` are still listed as-is.
- **Managed sockets can be probed directly**, even if `/proc` could not map
  them. Unmanaged sockets are probed only when `/proc` proves an `nvim`
  process owns them (A7). This keeps the safety rule: never connect to a
  socket that is not a Neovim server.
- **Stale socket removal** happens during discovery (managed paths only) and
  during spawn when the probe returns connection refused or ENOENT.
- **Implicit session names** (no `-n`/`-p`/`-r`) walk `project-2`,
  `project-3`, ... until a free socket path is found; explicit names collide
  with `session 'NAME' already exists (SOCK)`.
- **fzf key binding** uses `--expect=ctrl-x,ctrl-n,alt-m`; bare Enter is the
  empty key. Esc and Ctrl-C (exit 1 / 130) cancel with exit 0 from the hub.
- **Signals**: the hub catches SIGINT/SIGQUIT and discards them while the
  child runs (using `signal.Notify`, never `signal.Ignore`). SIGHUP/SIGTERM
  forward to the child and exit 129/143.
- **Fast-fail guard**: three consecutive client exits under
  `FastFailWindow` print `client exited immediately; is the server alive?`
  and exit 1.
- **`--load` matching** treats patterns without `/` as basenames and supports
  `**` segments; the original `find -name '**/*.c'` never matched.
- **Dormant discovery** accepts `.git` as a directory or a file (worktrees).
- **Timeouts** are `config.ProbeTimeout` (150 ms) for probes,
  `config.SpawnTimeout` (3 s) for readiness, `config.KillGrace` (2 s) per kill
  stage.
