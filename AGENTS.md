# AGENTS

Operational guide for maintaining `oci-context` daemon behavior and auth monitoring.

## Scope
- Keep instructions generic and environment-agnostic.
- Do not include personal hostnames, usernames, or absolute machine-specific paths.

## Daemon Behavior
- Daemon supports auth maintenance when started with `--auto-refresh`.
- Validation interval: `--validate-interval`.
- Refresh interval for `security_token`: `--refresh-interval`.
- On repeated failures, daemon applies exponential backoff and rate-limits error logs.

## Monitored Contexts
- Monitored contexts are configured in `options.daemon_contexts`.
- If list is empty, daemon falls back to `current_context`.
- Manage list via CLI:
  - `oci-context daemon monitor list`
  - `oci-context daemon monitor add <context...>`
  - `oci-context daemon monitor remove <context...>`
  - `oci-context daemon monitor clear`

## Status and Diagnostics
- Runtime status:
  - `oci-context daemon auth-status [--context <name>]`
  - `oci-context daemon doctor [--context <name>]`
  - `oci-context auth show --context <name>`
- If using `security_token`, ensure the target OCI profile has valid token/session material.
- If refresh fails persistently, re-authenticate (`oci session authenticate ...`).

## Agent Contract
- Use JSON output for automation wherever the CLI supports it, including
  `oci-context status -o json`, `oci-context export --format json`,
  `oci-context auth ensure --output json`, and
  `oci-context auth show --output json`.
- Treat JSON field names as stable contract. Prefer additive fields and document
  any breaking output change before relying on it in workflows or scripts.
- Preferred validation commands are `make fmt`, `make vet`, `make test`,
  `make lint-workflows`, and `make validate-workflows`.
- Release behavior is tag-driven: `v*` tags publish through GoReleaser, and the
  `auto-release` workflow can create semantic tags from Conventional Commit
  subjects on `main` while skipping commits that modify workflows.

## Demo Assets
- The README terminal capture is generated from `docs/demo/oci-context.tape` with VHS.
- Keep the README focused on product usage; implementation details for regenerating the capture belong here.
- Use fictional examples in demo assets, currently `dev`, `us-phoenix-1`, and sample OCID-shaped placeholders.
- After changing demo scripts or tapes, run:
  - `vhs validate docs/demo/oci-context.tape`
  - `vhs docs/demo/oci-context.tape`

## Background Service
- macOS: install/reload launchd daemon in one step:
  - `oci-context daemon install`
- specific targets: `oci-context daemon install launchd|sleepwatcher|hammerspoon`
- macOS: quick restart + nudge when returning to machine:
  - `oci-context daemon up`
  - aliases: `oci-context daemon recover`, `oci-context daemon fix`
- macOS: generate launchd plist with:
  - `oci-context daemon install launchd ...` (or legacy `daemon launchd generate`)
- macOS: optional actionable wake notifications with Hammerspoon:
  - `oci-context daemon install hammerspoon`
  - `oci-context auth notify [--context <name>]`
- After reinstalling/upgrading the binary, restart the launchd job so the daemon does not keep serving stale code:
  - `launchctl kickstart -k gui/$(id -u)/<launchd-label>`
- Linux: use `oci-context daemon install systemd` (or a manual `systemd --user` service).
- Use `--verbose` on daemon setup/diagnostic commands to print underlying system commands.

## Bootstrap
- Top-level bootstrap command:
  - `oci-context setup`
- Includes config + daemon bootstrap by default; auth setup is opt-in via `--with-auth`.
- Prefer a packaged/static binary path for service definitions rather than `go run`.

## Documentation Expectations
- README should stay aligned with current daemon commands and IPC methods.
- Any daemon feature change should update:
  - `README.md`
  - `AGENTS.md`
  - relevant tests
