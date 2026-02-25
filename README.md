# oci-context

A daemon + CLI/TUI to manage OCI context (profile, tenancy, compartment, region) akin to kubectl contexts. Designed to work with other tools (e.g., bastion-session, oci-secrets-courier/foundry) by exposing the current context over a local socket or via `oci-context export`.

## Components
- **Daemon (oci-contextd)**: maintains current context, serves a Unix socket with JSON RPC-like API for get/set/list/status/watch.
- **CLI/TUI (oci-context)**: manage contexts, switch current context, export env/JSON, control daemon, and pick via TUI selector.

### TUI controls (quick reference)
- `/` to start filtering the current list; hotkeys are suppressed while typing.
- `Enter` applies the filtered list and in-region modes stages the selection.
- `Space` stages/highlights the current row (pending save); staged items show in magenta.
- `Ctrl+S` saves the staged/current selection from any mode.
- `r` / `R` open region picker (from contexts or compartments); `c` opens compartments; `t/T` opens tenancies.
- Navigation: `h/left/backspace` go up/back; `C` returns to contexts.
- Quit: `q`, `Esc`, or `Ctrl+C` exit immediately **without saving**.

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
Requests: `{ "method": "get_current" }`, `{ "method": "use_context", "name": "dev" }`, `{ "method": "list" }`, `{ "method": "add_context", "context": { ... } }`, `{ "method": "delete_context", "name": "dev" }`, `{ "method": "status" }`, `{ "method": "export", "format": "env|json" }`, `{ "method": "watch" }`.
Responses: `{ "ok": true, "data": ... }` or `{ "ok": false, "error": "..." }`. Watch streams events.

## CLI commands
- `oci-context init`
- `oci-context list`
- `oci-context current`
- `oci-context use <name>`
- `oci-context add`
- `oci-context set <name> --field value`
- `oci-context delete <name>`
- `oci-context status`
- `oci-context export --format env|json`
- `oci-context daemon start|stop|status`
- `oci-context tui`

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
- **Release** (`.github/workflows/release.yml`): on tag `v*` or manual dispatch; runs tests, builds binaries, creates GitHub Release with artifacts.
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
