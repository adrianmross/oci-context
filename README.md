# oci-context

A daemon + CLI/TUI to manage OCI context (profile, tenancy, compartment, region) akin to kubectl contexts. Designed to work with other tools by exposing context over a local socket or via `oci-context export`.

## Components
- **Daemon (oci-contextd / `oci-context daemon serve`)**: serves Unix socket APIs and can run background auth validation/refresh loops.
- **CLI/TUI (oci-context)**: manage contexts, switch current context, export env/JSON, control daemon, and pick via TUI selector.

## Install

One-line install (default: `oci-context` latest stable):

```sh
curl -sSL https://raw.githubusercontent.com/adrianmross/oci-context/main/install.sh | bash
```

Install daemon binary instead:

```sh
TOOL=oci-contextd curl -sSL https://raw.githubusercontent.com/adrianmross/oci-context/main/install.sh | bash
```

Install a specific version:

```sh
VERSION=v0.1.0 curl -sSL https://raw.githubusercontent.com/adrianmross/oci-context/main/install.sh | bash
```

### TUI controls (quick reference)
- `/` to start filtering the current list; hotkeys are suppressed while typing.
- `Enter` applies the filtered list and in-region modes stages the selection.
- `Space` stages/highlights the current row (pending save); staged items show in magenta.
- `Ctrl+S` or `q` saves the staged/current selection from any mode.
- **Hotkey casing rule:** in the main menu (`contexts`) use lowercase hotkeys; in submenus (`tenancies`, `compartments`, `regions`) use uppercase hotkeys.
- Main menu (`contexts`): `r` opens regions, `c` opens compartments, `t` opens tenancies.
- Submenus: `R` opens regions, `C` opens compartments, `T` opens tenancies, `P` returns to profiles/contexts.
- Navigation: `backspace` goes up/back.
- Quit without saving: `Esc` or `Ctrl+C`.

## Config
Config resolution
- Default path (global): `~/.oci-context/config.yml`
- Project-aware auto-detection (when `--config` not set and `-g/--global` not set): first match wins in this order
  - `./.oci-context.yml`
  - `./.oci-context.json`
  - `./.oci-context/config.yml`
  - `./.oci-context/config.json`
  - `./oci-context.yml`
  - `./oci-context.json`
  - `./oci-context/config.yml`
  - `./oci-context/config.json`

Global vs project selection
- `--config` always overrides
- `-g, --global` forces the global config (`~/.oci-context/config.yml`)
- Otherwise the first project-local file found (above) is used; if none found, fall back to global

```yaml
options:
  oci_config_path: ~/.oci/config
  socket_path: ~/.oci-context/daemon.sock
  default_profile: ""
  daemon_contexts: []
contexts:
  - name: dev
    profile: DEFAULT
    tenancy_ocid: ocid1.tenancy.oc1..aaaa
    compartment_ocid: ocid1.compartment.oc1..bbbb
    region: us-phoenix-1
    user: alice@example.com
    notes: dev tenancy
  - name: prod
    profile: PROD
    tenancy_ocid: ocid1.tenancy.oc1..cccc
    compartment_ocid: ocid1.compartment.oc1..dddd
    region: us-ashburn-1
current_context: dev
```

## IPC API (Unix socket, framed JSON)
Requests include:
- `{ "method": "get_current" }`
- `{ "method": "use_context", "name": "dev" }`
- `{ "method": "list" }`
- `{ "method": "add_context", "context": { ... } }`
- `{ "method": "delete_context", "name": "dev" }`
- `{ "method": "export", "format": "env|json" }`
- `{ "method": "auth_status", "name": "dev" }` (name optional; defaults to current)

Responses: `{ "ok": true, "data": ... }` or `{ "ok": false, "error": "..." }`.

## CLI commands
- `oci-context --version` or `oci-context -v`
- `oci-context init`
- `oci-context list`
- `oci-context current`
- `oci-context oci -- <oci args...>` (runs OCI CLI with current context defaults)
- `oci-context use <name>`
- `oci-context add`
- `oci-context set <name> --field value`
- `oci-context delete <name>`
- `oci-context status`
- `oci-context export --format env|json`
- `oci-context auth methods|show|set|set-user|login|refresh|validate|setup|notify`
- `oci-context daemon serve [--auto-refresh --validate-interval 5m --refresh-interval 15m]`
- `oci-context daemon install` (macOS one-shot install/reload for launchd)
- `oci-context daemon auth-status [--context <name>]`
- `oci-context daemon nudge [--context <name>]`
- `oci-context daemon monitor list|add|remove|clear`
- `oci-context daemon launchd generate` (macOS)
- `oci-context daemon sleepwatcher install` (macOS wake hook automation)
- `oci-context daemon hammerspoon install` (macOS actionable wake notifications)
- `oci-context auth notify` (macOS manual trigger)
- `oci-context tui`

### OCI CLI Defaults (Transparent `oci` Usage)
To run plain `oci ...` commands without repeatedly passing `--profile`, `--region`, and `--compartment-id`, load managed OCI CLI defaults once per shell:

```sh
eval "$(oci-context export -f oci-env)"
```

This sets:
- `OCI_CLI_RC_FILE` to a managed rc file updated from your current context
- `OCI_CLI_CONFIG_FILE` to your configured OCI config path

After that, when you run `oci-context use ...` (or save from TUI), the managed OCI CLI defaults are refreshed automatically.

### current
Prints only the current context name.

```
$ oci-context current
dev
```

### status
Shows current context details with friendly names by default.

Default human-friendly multiline (omits profile line when context == profile):

```
$ oci-context status
context: dev
profile: DEFAULT
tenancy: My Tenancy (ocid1.tenancy.oc1..aaaa)
compartment: My Compartment (ocid1.compartment.oc1..bbbb)
user: Alice (ocid1.user.oc1..cccc)
region: us-phoenix-1
```

Plain OCIDs only (`-p`):

```
$ oci-context status -p
context=dev profile=DEFAULT tenancy=ocid1.tenancy.oc1..aaaa compartment=ocid1.compartment.oc1..bbbb user=ocid1.user.oc1..cccc region=us-phoenix-1
```

Single-line friendly with abbreviated OCIDs (`-o plain`):

```
$ oci-context status -o plain
context=dev profile=DEFAULT tenancy=My Tenancy (ocid1…aaaa) compartment=My Compartment (ocid1…bbbb) user=Alice (ocid1…cccc) region=us-phoenix-1
```

Structured outputs:

```
$ oci-context status -o json
{ "context": "dev", "profile": "DEFAULT", ... }

$ oci-context status -o yaml
context: dev
profile: DEFAULT
...
```

Errors:
- If no current context is set, returns: `no current context set`.
- If a bad `-o` value is provided, returns: `unsupported output format: <value>`.

## Integration
- Shell: `eval $(oci-context export --format env)` sets OCI_CLI_PROFILE, OCI_TENANCY_OCID, OCI_COMPARTMENT_OCID, OCI_REGION.
- Tools: read JSON via `oci-context export --format json` or query the socket.

## Daemon Auth Monitoring
The daemon can monitor and maintain auth for one or more contexts in the background.

- If `options.daemon_contexts` is empty, daemon monitors `current_context`.
- If `options.daemon_contexts` has entries, daemon monitors all listed contexts.
- `security_token` contexts: daemon validates and refreshes based on intervals.
- Other auth methods: daemon validates only.
- Failure handling uses exponential backoff and stderr rate-limiting to reduce noise.

Manage monitored contexts:

```sh
oci-context daemon monitor add dev prod
oci-context daemon monitor list
oci-context daemon monitor remove dev
oci-context daemon monitor clear
```

Trigger immediate maintenance without waiting for interval:

```sh
oci-context daemon nudge
oci-context daemon nudge --context dev
```

Run daemon with auth maintenance enabled:

```sh
oci-context daemon serve --auto-refresh --validate-interval 5m --refresh-interval 15m
```

Check runtime status:

```sh
oci-context daemon auth-status --context dev
oci-context auth show --context dev
```

## Background Service
### macOS (`launchd`)
One-shot install/reload (recommended):

```sh
oci-context daemon install --auto-refresh
```

This writes the launchd plist, (re)loads it, and kickstarts the job so the running daemon uses the current binary.

Generate a plist:

```sh
oci-context daemon launchd generate \
  --binary /path/to/oci-context \
  --config ~/.oci-context/config.yml \
  --auto-refresh \
  --validate-interval 5m \
  --refresh-interval 15m
```

Then load/start:

```sh
launchctl unload ~/Library/LaunchAgents/com.adrianmross.oci-context.daemon.plist 2>/dev/null || true
launchctl load ~/Library/LaunchAgents/com.adrianmross.oci-context.daemon.plist
launchctl start com.adrianmross.oci-context.daemon
```

After reinstalling/upgrading `oci-context`, restart the running launchd job so the daemon picks up the new binary. Otherwise, IPC commands can hit stale behavior (for example, `daemon nudge` returning `method not implemented` while help/docs show it):

```sh
launchctl kickstart -k gui/$(id -u)/com.adrianmross.oci-context.daemon
```

### sleep/wake automation with sleepwatcher
Install wake hook automation (restarts daemon + nudges auth checks on wake):

```sh
brew install sleepwatcher
oci-context daemon sleepwatcher install
```

### Actionable wake notifications with Hammerspoon (macOS)
Install managed Hammerspoon integration plus a wake hook script that sends clickable notifications. Clicking `Re-auth now` runs `oci session authenticate` for the affected profile.

```sh
brew install --cask hammerspoon
oci-context daemon hammerspoon install
```

Notes:
- This command writes/updates:
  - `~/.hammerspoon/oci_context.lua` (managed URL handler + auth task runner)
  - `~/.hammerspoon/init.lua` (adds `pcall(require, "oci_context")` when missing)
  - `~/.wakeup` (wake script that nudges daemon and raises actionable notifications on auth failures)
- If you prefer manual reload, run with `--reload=false` and then:

```sh
open -g 'hammerspoon://reloadConfig'
```

Manually trigger an actionable notification from CLI:

```sh
oci-context auth notify --context dev --reason "manual check"
oci-context auth notify --context dev --reason "manual check" --tenancy-name your-tenancy
```

`--tenancy-name` is optional. When omitted, `notify` uses the selected context's `tenancy_ocid`.
`--native-notify` defaults to false; enable it if you also want a native macOS notification.
If `terminal-notifier` is installed, `notify` uses it and clicking the native notification opens the Hammerspoon URL action.

### Linux (`systemd`, user service)
Example unit file (`~/.config/systemd/user/oci-context-daemon.service`):

```ini
[Unit]
Description=oci-context daemon
After=network-online.target

[Service]
ExecStart=/path/to/oci-context daemon serve --config %h/.oci-context/config.yml --auto-refresh --validate-interval 5m --refresh-interval 15m
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
```

Enable and start:

```sh
systemctl --user daemon-reload
systemctl --user enable --now oci-context-daemon
systemctl --user status oci-context-daemon
```
## Status
Work in progress.

---

## Development and CI/CD

### Local prerequisites
- Go (from `go.mod` go version; currently 1.25.6)
- `actionlint` (for workflow linting): `brew install actionlint` **or** `go install github.com/rhysd/actionlint/cmd/actionlint@latest`
- `act` (for local workflow dry runs): `brew install act` **or** `go install github.com/nektos/act@latest`

Check tool availability:
```sh
make tools
```

### Local validation
Runs formatting, vet, tests, actionlint, and an `act` dry run against a sample PR payload:
```sh
make validate
```
Targets:
- `make fmt`
- `make vet`
- `make test`
- `make lint-workflows`
- `make validate-workflows` (uses `.github/testdata/pull_request.json`)

### GitHub Actions workflows
- **CI** (`.github/workflows/ci.yml`): on push/PR to main/develop/release/**; runs gofmt (checks diff), go vet, go test, actionlint.
- **Release** (`.github/workflows/release.yml`): on tag `v*` or manual dispatch; validates tag, runs tests, and uses GoReleaser to publish cross-platform tarballs plus `checksums.txt`.
- **CD** (`.github/workflows/cd.yml`): manual `workflow_dispatch` with `env` input; placeholder deploy step to be customized.

### Release tagging flow
```sh
git tag v0.1.0
git push origin v0.1.0
```
This triggers the Release workflow, builds binaries, and attaches them to the GitHub release.

### Acting CI locally
Example dry-run of CI with act (requires Docker):
```sh
act pull_request --eventpath .github/testdata/pull_request.json --dryrun
```

### Repository
Private GitHub repo: https://github.com/adrianmross/oci-context
