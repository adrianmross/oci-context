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
- Includes config bootstrap and can run daemon + auth setup in one flow.
- Prefer a packaged/static binary path for service definitions rather than `go run`.

## Documentation Expectations
- README should stay aligned with current daemon commands and IPC methods.
- Any daemon feature change should update:
  - `README.md`
  - `AGENTS.md`
  - relevant tests
