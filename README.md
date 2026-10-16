# blixa

[![test](https://github.com/laszlolevi/blixa/actions/workflows/test.yml/badge.svg)](https://github.com/laszlolevi/blixa/actions/workflows/test.yml)
[![coverage](https://codecov.io/gh/laszlolevi/blixa/graph/badge.svg)](https://codecov.io/gh/laszlolevi/blixa)
[![release](https://img.shields.io/github/v/release/laszlolevi/blixa)](https://github.com/laszlolevi/blixa/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/laszlolevi/blixa)](go.mod)
[![platform](https://img.shields.io/badge/platform-macOS-lightgrey)](#install)
[![license](https://img.shields.io/github/license/laszlolevi/blixa)](LICENSE)

A terminal UI for npm, pnpm, yarn & bun projects: git pull / checkout, installing
dependencies, and running arbitrary package scripts, each with the project's own package
manager. Scripts keep running in a background daemon even after the TUI closes; blixa
automatically detects their ports.

## Install

macOS (Apple Silicon or Intel):

```sh
curl -fsSL https://raw.githubusercontent.com/laszlolevi/blixa/main/install.sh | sh
```

This downloads the latest release, verifies its checksum and installs the binary to
`~/.local/bin/blixa` (if that folder isn't on your `PATH`, the installer tells you what to add).
Run the same command again to update. Options, as environment variables for `sh`:

```sh
curl -fsSL https://raw.githubusercontent.com/laszlolevi/blixa/main/install.sh | BLIXA_VERSION=v0.1.0 sh
curl -fsSL https://raw.githubusercontent.com/laszlolevi/blixa/main/install.sh | BLIXA_INSTALL_DIR=/usr/local/bin sh
```

From source (needs Go):

```sh
go build -o ~/.local/bin/blixa .
```

### Releasing

Push a `v*` tag (e.g. `git tag v0.1.0 && git push origin v0.1.0`); the `release` workflow
builds the macOS binaries and publishes them as a GitHub release, which `install.sh` downloads.
The `test` workflow runs the test suite on macOS for every push to `main`; check that it passed
before tagging (the release workflow itself only cross-compiles).

## Usage

```bash
blixa            # TUI (starts the daemon if needed)
blixa daemon     # start the daemon manually (usually not needed)
blixa --root <dir>  # set (or change) the projects root directory
```

On the very first run, if no config file exists yet and `--root` wasn't given, blixa asks
"Where are your projects?" (suggesting the current directory, or its parent if it is a
project itself) and saves the answer to the config. Afterwards the root can be changed inside
the TUI (see [Folders](#folders)) or with `--root`.

When blixa starts and no daemon is running, it checks GitHub for a newer release
(pre-releases don't count) and offers to update. On yes, it downloads the release, verifies
its checksum, replaces its own binary and restarts as the new version. If the check fails
(e.g. offline), blixa starts as usual without asking. Local builds (`dev …`, `+dirty`)
never check.

### What counts as a project

A project is any direct subdirectory of the root that contains a `package.json`. Git is
optional: pull/checkout/branch status are only available for projects that are also a git
repository (i.e. have a `.git` directory); a non-git project still shows up, just without
those operations.

A project's scripts come from the root `package.json`, plus its workspaces:
- the `packages` list of `pnpm-workspace.yaml`, or
- the `workspaces` field of `package.json` (npm/yarn/bun, as an array or as `{"packages": [...]}`), or
- `apps/*` and `packages/*`, when neither exists.

### Package managers

npm, pnpm, yarn and bun are supported. The package manager is detected per project — from the
`packageManager` field of `package.json` (corepack) first, then the lockfile (`pnpm-lock.yaml`,
`yarn.lock`, `bun.lock`/`bun.lockb`, `package-lock.json`), npm by default — and used for both
running scripts (`<pm> run <script>`) and installs (`<pm> install`). The details frame shows a
project's package manager. If the detected one isn't installed, the task fails with
`blixa: <pm> is not installed`.

nvm is optional: when it's installed and the project has an `.nvmrc`, blixa switches to that
Node version first; otherwise the system node on `PATH` is used.

## The screen

```
▰ blixa ─ ~/workspace   daemon ●
╭─ folders ────────────────────────────────────────────────────────────╮
│  ~/oss │ ~/workspace          ↑ folders  tab switch  o open  W close │
╰──────────────────────────────────────────────────────────────────────╯
╭─ projects ───────────────────────────────────────────────────────────╮
│ ▾ shop-api      feature/checkout-redesign ✎ ↓2                       │
│     dev         ● running     :3000 +2    11m33s                     │
╰──────────────────────────────────────────────────────────────────────╯
╭─ shop-api › dev ─────────────────────────────────────────────────────╮
│ primary port  :3000 http  →  http://localhost:3000                   │
│ other ports   :3001 ws  :9229 debug                                  │
│ usage         cpu 12%  ·  mem 340 MB                                 │
╰──────────────────────────────────────────────────────────────────────╯
 s start  x stop  r restart  l log  p pull  c checkout  …  ? help  q quit
```

The keys are always on the last line, the version in its bottom right corner; `?` lists all keys.

### Folders

Every root used so far is a tab in the top frame. `tab` / `shift+tab` switch between them;
`↑` on the first project row moves into the tabs, where `←` / `→` switch (the list below
follows right away) and `↓` / `enter` / `esc` go back. `o` opens another folder (a path input,
`tab` completes), `W` closes the current tab (after `y` to confirm). Each root keeps its
own script selections; running tasks keep running when you switch. While the tabs have the
focus, the details frame shows the folder: its path, how many projects it has (and how many
with selected scripts), and the tasks running in it with their ports.

### Projects

A project row shows the git branch (with `↑` ahead, `↓` behind, `✎` uncommitted changes) —
as wide as the frame allows — and, while it runs, failed, or for a few seconds after it
succeeded, the last pull / checkout / install. Every 5 minutes (and shortly after start)
blixa fetches the current root's git projects in the background, so `↑` / `↓` stay honest;
the fetch never prompts for credentials and gives up after 2 minutes. `D` on a "missing"
project (one still listed in `config.json` whose folder no longer exists under the root)
removes it from the config.

### Details

The bottom frame has a fixed height (nothing moves when something shows up) and describes
the row under the cursor:
- a task: its **primary port** (the first one it opened, usually the dev server; a debugger
  port such as `node --inspect`'s only if nothing else is open) with its URL, the other
  ports, and its CPU (percent of one core, summed over the process group) and memory; if it
  crashed or exited with an error, the log's last lines instead. Next to each port is what
  speaks on it — blixa probes every new port once: `http`, `https`, `ws`, `wss`, `debug`,
  `grpc`, `mqtt`, `ssh`, `mysql`, `smtp`, `ftp`, `pop3`, `imap`, or `tcp` if none of these.
- a project: the full branch name, the package manager and the last operation.

The http(s) ports are terminal hyperlinks (OSC 8): ⌘-click one to open it in the browser (iTerm2,
Ghostty, WezTerm, Kitty; Terminal.app only recognizes the full URL).

### Log view

Line numbers (`#` toggles them), `/` search with highlighted matches (`n`: next), `f` follow.
Scroll with the arrows, page keys, or the mouse wheel / touchpad. The mouse is captured only
in the log view, so select text there with ⌥-drag (iTerm2) / fn-drag (Terminal.app) /
⇧-drag (Ghostty, Kitty, WezTerm).

### Checkout

`c` opens two lists, local branches on top and remote ones below, each with its own cursor;
`tab` / `←` / `→` switch between them, typing filters both. Both are sorted by name and show
each branch's last commit's age; the current branch is marked with its `↑` / `↓`, a remote
branch with `= local` / `≠ local` when a local branch of the same name exists. `enter` on a
remote branch creates the local one, or resets an existing local one to it (e.g. after the
remote was rebased) — asking first if that would drop commits found only locally.

## Files

- Config: `~/.config/blixa/config.json` (`root`, and the selected scripts per project, kept
  separately for every root — switching folders and back restores the selections)
- State and logs: `~/.local/state/blixa/` (`state.json`, `logs/`, `daemon.log`)

## License

MIT, see [LICENSE](LICENSE).
