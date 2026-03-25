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
  - `oci-context auth show --context <name>`
- If using `security_token`, ensure the target OCI profile has valid token/session material.
- If refresh fails persistently, re-authenticate (`oci session authenticate ...`).

## Background Service
- macOS: generate launchd plist with:
  - `oci-context daemon launchd generate ...`
- macOS: optional actionable wake notifications with Hammerspoon:
  - `oci-context daemon hammerspoon install`
  - `oci-context daemon hammerspoon notify [--context <name>]`
- After reinstalling/upgrading the binary, restart the launchd job so the daemon does not keep serving stale code:
  - `launchctl kickstart -k gui/$(id -u)/<launchd-label>`
- Linux: use a `systemd --user` service.
- Prefer a packaged/static binary path for service definitions rather than `go run`.

## Documentation Expectations
- README should stay aligned with current daemon commands and IPC methods.
- Any daemon feature change should update:
  - `README.md`
  - `AGENTS.md`
  - relevant tests
