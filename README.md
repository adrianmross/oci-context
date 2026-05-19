# oci-context

OCI contexts for humans, scripts, and agents.

`oci-context` gives OCI the same day-to-day ergonomics that `kubectl config
use-context` gives Kubernetes: switch profile, tenancy, compartment, region, and
auth state once, then let tools read the current context safely.

![oci-context terminal demo](docs/assets/oci-context-demo.gif)

## What It Does

- stores named OCI contexts in YAML or JSON
- switches the current OCI profile, region, tenancy, and compartment
- validates and refreshes OCI auth
- exposes script-safe JSON/YAML/text output
- provides a daemon socket for local tools
- powers higher-level tools such as `bastion-session` and `oci-bassh`

## Install

Homebrew is the preferred install path:

```bash
brew tap adrianmross/tap
brew install oci-context
```

The Homebrew binaries are installed at:

```bash
/opt/homebrew/bin/oci-context
/opt/homebrew/bin/oci-contextd
```

Source install:

```bash
curl -sSL https://raw.githubusercontent.com/adrianmross/oci-context/main/install.sh | bash
```

Install the daemon binary instead:

```bash
TOOL=oci-contextd curl -sSL https://raw.githubusercontent.com/adrianmross/oci-context/main/install.sh | bash
```

By default the installer writes to `/usr/local/bin`. Override it with `PREFIX`:

```bash
PREFIX="$HOME/.local" curl -sSL https://raw.githubusercontent.com/adrianmross/oci-context/main/install.sh | bash
```

Install a specific release:

```bash
VERSION=v0.14.0 curl -sSL https://raw.githubusercontent.com/adrianmross/oci-context/main/install.sh | bash
```

## Quickstart

Create or update local config:

```bash
oci-context init
oci-context add
oci-context use dev
```

Check the active context:

```bash
oci-context current
oci-context status
```

Make sure auth is ready before automation:

```bash
oci-context auth ensure --output json
```

Inspect local metadata without calling OCI:

```bash
oci-context version -o json
oci-context paths -o json
oci-context status --cached -o json
```

## Config Paths

Global config:

```text
~/.oci-context/config.yml
```

Project-local config is auto-detected when `--config` and `--global` are not
set. First match wins:

```text
./.oci-context.yml
./.oci-context.json
./.oci-context/config.yml
./.oci-context/config.json
./oci-context.yml
./oci-context.json
./oci-context/config.yml
./oci-context/config.json
```

Selection rules:

- `--config <path>` always wins
- `--global` forces `~/.oci-context/config.yml`
- otherwise the first project-local file wins
- if no project-local file exists, global config is used

Use `oci-context paths -o json` to see the selected path, selection source,
project candidates, configured OCI config path, socket path, and any nonfatal
config load error.

Example config:

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
current_context: dev
```

## Commands

```bash
oci-context --version
oci-context version -o text|json|yaml
oci-context paths -o text|json|yaml
oci-context init
oci-context list
oci-context current
oci-context use <name>
oci-context add
oci-context set <name> --field value
oci-context delete <name>
oci-context status --cached -o json
oci-context doctor --output json
oci-context oci -- <oci args...>
oci-context auth methods|show|set|set-user|login|refresh|ensure|validate|setup|notify
oci-context daemon serve
oci-context daemon up
oci-context daemon doctor
oci-context tui
```

## Auth Readiness

Use `auth ensure` before OCI-dependent automation. It validates the selected
context, refreshes `security_token` auth when possible, and returns a clear
structured result:

```bash
oci-context auth ensure --output json
oci-context auth show --output json
oci-context auth methods --output json
oci-context doctor --output json
```

If validation and refresh cannot recover a security token, the command reports
`login_required: true`. To allow an interactive browser login as part of the
same command:

```bash
oci-context auth ensure --login
```

For non-interactive automation:

```bash
oci-context --no-interactive auth ensure --login --output json
```

## OCI CLI Defaults

To run plain `oci ...` commands without repeatedly passing profile, region, and
compartment, load managed OCI CLI defaults once per shell:

```bash
eval "$(oci-context export -f oci-env)"
```

This sets:

- `OCI_CLI_RC_FILE` to a managed rc file updated from your current context
- `OCI_CLI_CONFIG_FILE` to your configured OCI config path

After that, `oci-context use ...` and TUI saves refresh the managed OCI CLI
defaults automatically.

## TUI Controls

- `/` starts filtering
- `Enter` applies the filtered list and stages in-region selections
- `Space` stages or highlights the current row
- `Ctrl+S` or `q` saves
- `Esc` or `Ctrl+C` quits without saving
- `backspace` goes back
- main menu hotkeys are lowercase: `r`, `c`, `t`
- submenu hotkeys are uppercase: `R`, `C`, `T`, `P`

## Agent Contract

Stable automation output is JSON. Agents should prefer `--output json`,
`-o json`, or `--format json` for supported commands such as `status`, `paths`,
`version`, `export`, `auth ensure`, `auth show`, and daemon status commands.

Use `status --cached -o json`, `auth show --output json`, and
`auth ensure --output json` for ordinary inspection. Use `export` only when the
task is explicitly to export shell environment settings or hand a context to
another process.

## IPC API

The daemon serves framed JSON over a Unix socket.

Example requests:

```json
{ "method": "get_current" }
{ "method": "use_context", "name": "dev" }
{ "method": "list" }
{ "method": "export", "format": "env" }
{ "method": "auth_status", "name": "dev" }
```

Responses use:

```json
{ "ok": true, "data": {} }
```

or:

```json
{ "ok": false, "error": "..." }
```
