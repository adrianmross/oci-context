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

Emit an OBP/OABCS token for another command without persisting it in that
tool's config:

```bash
oci-context auth token --service obp --format raw
```

Tools such as `ochain` can use that as a credential-command bridge:

```json
{
  "auth": {
    "tokenCommand": "oci-context auth token --service obp --format raw"
  }
}
```

Token services are generic OAuth definitions. The shipped `obp` service reads
the OChain-provided target metadata and uses `flow: auto`: Authorization Code is
preferred when the issuer advertises an authorization endpoint, and Device Code
is used only when that is the available interactive flow. Define additional
services in config when another tool should use a different issuer, client,
scope, redirect URL, or flow:

```yaml
current_service: obp
token_services:
  - name: obp
    type: oauth
    flow: authorization-code
    issuer_envs:
      - OCHAIN_OBP_AUTH_ISSUER
      - OBP_OAUTH2_ISSUER
    client_id: obp
    client_secret_env: OCHAIN_OBP_AUTH_CLIENT_SECRET
    scope_envs:
      - OCHAIN_OBP_AUTH_SCOPE
      - OCHAIN_OBP_PLATFORM
    authorization_endpoint_env: OCHAIN_OBP_AUTH_AUTHORIZATION_ENDPOINT
    token_endpoint_env: OCHAIN_OBP_AUTH_TOKEN_ENDPOINT
    redirect_url_env: OCHAIN_OBP_AUTH_REDIRECT_URL
```

With `current_service` set in a global or project-local `.oci-context.yml`,
interactive service login does not need stdin or flags:

```bash
oci-context auth login
oci-context auth token --no-login --format raw
```

`oci-idm` materializes `oci-context-token-services.yml` and
`oci-context.handoff.json` files for planned Identity Domains apps. Import
either handoff shape directly:

```bash
oci-context service add \
  --file ./idm-artifacts/oci-context-token-services.yml \
  --set-current

oci-context service list
```

`oci-context auth service import --file ...` remains available for older
scripts. The top-level `service add` command also reads stdin, which makes the
auth-target handoff a plain JSON/YAML pipe:

```bash
oci-idm clone app --flow authorization-code --name hebe-obp-user |
  oci-context service add --set-current

oci-context auth login
```

`oci-idm get defaults` can also pipe the current target metadata directly into
`oci-context auth login`. `oci-context` consumes the service name, issuer, and
scope from stdin, merges those non-secret values into the named token service,
then runs the interactive OAuth login and caches the resulting token:

```bash
oci-idm get defaults --service obp |
  oci-context auth login

oci-context auth token --service obp --no-login --format raw
```

For the common Red Wiz OABCS target, select the OCI context for `oabcs1` in the
default domain and configure the chaincode deploy environment for
`pmdemo/adrian/did` on channel `testnet`. The token command reads
`OCHAIN_OBP_AUTH_ISSUER`, `OCHAIN_OBP_AUTH_CLIENT_ID`, and
`OCHAIN_OBP_AUTH_SCOPE` from the caller environment, so OChain can resolve the
target and oci-context can own the browser login.

For Oracle Identity Cloud Service domains that do not expose a Device Code
endpoint, configure a CLI-capable IDCS application with Authorization Code and
a loopback redirect URL, then either pipe target defaults into login or force
the lower-level token flow locally:

```bash
export OCHAIN_OBP_AUTH_REDIRECT_URL=http://127.0.0.1:8180/callback
oci-idm get defaults --service obp |
  oci-context auth login

oci-context auth token \
  --service obp \
  --flow authorization-code \
  --redirect-url "$OCHAIN_OBP_AUTH_REDIRECT_URL" \
  --format raw
```

Register the same redirect URL on the IDCS confidential application. If the
client requires a secret, provide it through `OCHAIN_OBP_AUTH_CLIENT_SECRET` or
service config rather than storing it in the downstream tool. stdout contains
the bearer token and must be treated as secret material; browser instructions
are written to stderr.

The default OBPCS cloud-service application is normally a CloudGate web
application. Its redirect is service-owned, for example
`https://<hostid>/cloudgate/v1/oauth2/callback`. That callback is correct for
browser access to the OBPCS service, but it is not a CLI callback:
CloudGate receives and consumes the authorization code, so `oci-context` cannot
exchange it for a bearer token. For command handoff, use a separate IDCS OAuth
client that is allowed to request the OBP REST proxy scope and has a registered
loopback redirect such as `http://127.0.0.1:8180/callback`, or use a trusted
non-interactive flow such as client credentials or JWT assertion when that is
the intended identity.

Non-interactive OAuth flows are first-class and can run under `--no-login`.
Use `jwt-client-credentials` for service accounts whose IDCS app trusts a
client assertion:

```bash
oci-context auth token \
  --service obp \
  --flow jwt-client-credentials \
  --token-endpoint "$OCHAIN_OBP_AUTH_TOKEN_ENDPOINT" \
  --client-id "$OCHAIN_OBP_AUTH_CLIENT_ID" \
  --scope "$OCHAIN_OBP_AUTH_SCOPE" \
  --client-assertion-command ./mint-client-assertion.sh \
  --no-login \
  --format raw
```

When the client assertion should be signed locally, provide a PEM private key
and optional key id instead of a command:

```bash
oci-context auth token \
  --service obp \
  --flow jwt-client-credentials \
  --token-endpoint "$OCHAIN_OBP_AUTH_TOKEN_ENDPOINT" \
  --client-id "$OCHAIN_OBP_AUTH_CLIENT_ID" \
  --scope "$OCHAIN_OBP_AUTH_SCOPE" \
  --private-key-file ./idcs-client.key \
  --key-id "$OCHAIN_OBP_AUTH_KEY_ID" \
  --no-login \
  --format raw
```

For OCI Identity Domains, locally signed client assertions normally use
`https://identity.oraclecloud.com/` as the assertion audience. `oci-context`
uses the token endpoint by default for generic OAuth servers, but if an OCI
Identity Domains token endpoint rejects that default with an invalid assertion
audience error, it retries once with the OCI audience. You can make that
explicit with `--jwt-audience https://identity.oraclecloud.com/`.

Use `jwt-bearer` when a trusted issuer can assert a user or subject that the
identity domain maps to a service-authorized identity:

```bash
oci-context auth token \
  --service obp \
  --flow jwt-bearer \
  --token-endpoint "$OCHAIN_OBP_AUTH_TOKEN_ENDPOINT" \
  --client-id "$OCHAIN_OBP_AUTH_CLIENT_ID" \
  --scope "$OCHAIN_OBP_AUTH_SCOPE" \
  --assertion-command ./mint-user-assertion.sh \
  --no-login \
  --format raw
```

Use `token-exchange` for federated workload identity, such as a GitHub Actions
OIDC token, Kubernetes projected service account token, or another external
workload JWT:

```bash
oci-context auth token \
  --service obp \
  --flow token-exchange \
  --token-endpoint "$OCHAIN_OBP_AUTH_TOKEN_ENDPOINT" \
  --client-id "$OCHAIN_OBP_AUTH_CLIENT_ID" \
  --scope "$OCHAIN_OBP_AUTH_SCOPE" \
  --subject-token-command ./mint-workload-token.sh \
  --requested-token-type urn:ietf:params:oauth:token-type:access_token \
  --no-login \
  --format raw
```

OAuth tokens are cached by service and are reused under `--no-login` when still
valid. Use `--no-cache` to force a fresh token request and avoid updating the
cached token, for example when validating a new IDCS app, role grant, or signing
key without disturbing a known-good cached token.

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

When the selected config path ends in `.json`, writes preserve JSON encoding.
Other config paths are written as YAML. Config writes are protected by a file
lock and atomic rename.

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
oci-context daemon repair --all --monitor dev
oci-context daemon doctor
oci-context setup daemon --all --monitor dev
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

Structured auth results include both detailed booleans and a small decision
surface for wrappers:

- `ready`
- `action_required`
- `action` (`none`, `login`, or `check_auth`)
- `severity` (`ok` or `error`)

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

## Daemon Health

Install or refresh all macOS daemon integrations and monitor a context:

```bash
oci-context setup daemon --all --monitor dev
```

Equivalent daemon-focused form:

```bash
oci-context daemon repair --all --monitor dev
```

For a lightweight post-wake or pre-work check:

```bash
oci-context daemon auth-status
oci-context auth ensure --no-interactive
```

`daemon auth-status` includes a daemon-specific readiness contract:

- `ready`: validation currently proves auth is usable.
- `action_required`: something needs operator or automation action.
- `action`: `none`, `nudge`, `login`, or `check_auth`.
- `severity`: `ok`, `warning`, or `error`.
- `reason`: human-readable explanation for the decision.

For `security_token` contexts, a failed refresh is not by itself an action when
validation still succeeds. This avoids wake notifications for healthy sessions
that were not freshly refreshed.

Use structured daemon diagnostics for automation:

```bash
oci-context daemon doctor --output json
oci-context daemon nudge --output json
oci-context daemon recover --output json
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
