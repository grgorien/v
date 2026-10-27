<p align="center">
  <img src="assets/_logo.v.png" alt="v logo" width="120" height="120">
</p>

<p align="center">
  <b>Use vim motions and natively manage your socks.</b><br>
  A Neovim session manager and lightweight multiplexer written in Go.
</p>

<p align="center">
  <a href="#install">Install</a> •
  <a href="#commands">Commands</a> •
</p>

---

`v` runs as the foreground process of a single terminal window. It acts as an interactive picker for Neovim sessions, attaches the chosen server using `nvim --server SOCK --remote-ui`, and seamlessly returns to the picker whenever the client exits. 

Because servers run headless and detached, closing your terminal window loses zero state.

---

## Install

Installs `v` into `$PREFIX/bin` (default `/usr/local`):

```bash
make install
```

### Runtime Requirements
* **`nvim`** and **`fzf`** available on your `$PATH`.
* **`stty`** (used to safely save and restore terminal states).

---

## Neovim Mappings

The hub relies on your Neovim configuration to handle detaching gracefully. Ensure your `init.lua` maps `<leader>d` and terminal-mode `<C-\><C-d>` to `:detach`:

```lua
vim.keymap.set('n', '<leader>d', ':detach<CR>', { silent = true })
vim.keymap.set('t', '<C-\\><C-d>', '<C-\\><C-n>:detach<CR>', { silent = true })
```
When the client detaches or the server quits, the hub automatically redisplays the session picker.

---

## Commands

| Command | Effect |
| :--- | :--- |
| `v` | Open hub (all rows: live + dormant repos) |
| `v -j`, `v --jump` | Same as `v` |
| `v -a`, `v --attach` | Open hub (live rows only) |
| `v -l`, `v --list` | Print session table (`--json` for JSON output) |
| `v -k`, `v --kill` | Interactive multi-select to force-quit sessions |
| `v -n NAME` | Create new session (`project` or `project.role`) |
| `v -p P` / `v -r R` | Specify project / role for a new session |
| `v --load PATTERN [DIR]` | New session with up to 40 matching files |
| `v [DIR]` | New session started in `DIR` |
| `v [FILES...]` | New session with files (or open inside current nvim instance) |
| `v -t X` | Inside nvim: send files to session `X` |
| `v --mv NAME TARGET` | Move a session to a new project or role |
| `v --detached` | Create session without attaching, print socket path |
| `v --startuptime` | Pass `--startuptime` to the new server |
| `v -h`, `v --help` | Show usage information |

---

## State & Storage

All operational data lives under `${XDG_STATE_HOME:-$HOME/.local/state}/v`:
* `sockets/` — Active UNIX domain sockets
* `logs/` — Server logs and debugging output
* `mru` — Most recently used tracking

---

<details>
<summary><b>Design Decisions (Click to expand)</b></summary>

* **Sanitization:** Sanitization does not append trailing underscores (`_`). The original bash implementation used a here-string that turned trailing newlines into `_`. Go's `proj.Sanitize("main")` cleanly returns `main` (though legacy sockets ending in `_` remain fully compatible).
* **Socket Safety & Probing:** Managed sockets are probed directly. Unmanaged sockets are probed only when `/proc` confirms an active `nvim` process owns them, ensuring `v` never accidentally connects to an alien socket.
* **Stale Socket Cleanup:** Automatically purged during discovery (managed paths) and during spawn attempts if a probe returns connection refused or `ENOENT`.
* **Implicit Naming:** Omitting `-n`/`-p`/`-r` automatically walks increments (`project-2`, `project-3`, etc.) until a free path is found. Explicit name collisions return a clear error: `session 'NAME' already exists (SOCK)`.
* **fzf Key Bindings:** Binds `--expect=ctrl-x,ctrl-n,alt-m` (bare Enter is empty). Esc and `Ctrl-C` cancel cleanly with exit code `0` from the hub.
* **Signal Forwarding:** The hub intercepts `SIGINT`/`SIGQUIT` and suppresses them while a child process runs using `signal.Notify`. `SIGHUP`/`SIGTERM` are correctly forwarded to the child.
* **Fast-Fail Guard:** Three consecutive rapid client exits within `FastFailWindow` trigger `client exited immediately; is the server alive?` and exit with status `1`.
* **Pattern Matching (`--load`):** Treats patterns without `/` as basenames and fully supports `**` glob segments.
* **Dormant Discovery:** Seamlessly recognizes `.git` as either a traditional directory or a git worktree file.
* **Timeouts:** Configured for `config.ProbeTimeout` (150 ms), `config.SpawnTimeout` (3 s), and `config.KillGrace` (2 s) per termination stage.

</details>

---

## License

Distributed under the MIT License. See `LICENSE` for more information.
