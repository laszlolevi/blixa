# blixa

A terminal UI for npm projects: git pull / checkout, npm install, and running
arbitrary npm scripts. Scripts keep running in a background daemon even after the TUI
closes; blixa automatically detects their ports.

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
Run `go test ./...` on macOS before tagging (the CI only cross-compiles).

## Usage

```bash
blixa            # TUI (starts the daemon if needed)
blixa daemon     # start the daemon manually (usually not needed)
blixa --root <dir>  # set (or change) the projects root directory
```

On the very first run, if no config file exists yet and `--root` wasn't given, blixa asks
"Where are your projects?" (suggesting the current directory, or its parent if it is a
project itself) and saves the answer to the config. Afterwards, `--root` can be used at any
time to change the saved root.

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
running scripts (`<pm> run <script>`) and installs (`<pm> install`). The project row shows the
package manager when it isn't npm. If the detected one isn't installed, the task fails with
`blixa: <pm> is not installed`.

nvm is optional: when it's installed and the project has an `.nvmrc`, blixa switches to that
Node version first; otherwise the system node on `PATH` is used.

- Config: `~/.config/blixa/config.json` (`root`, and the selected scripts per project, kept
  separately for every root — switching with `--root` and back restores the selections)
- State and logs: `~/.local/state/blixa/` (`state.json`, `logs/`, `daemon.log`)
- Keys: `?` inside the TUI.
- `D`: on a "missing" project (one that's still listed in `config.json` but whose folder
  no longer exists under the root) removes it from the config.
