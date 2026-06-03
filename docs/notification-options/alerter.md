# Alerter Backend

This branch makes `oci-context auth notify` use `alerter` directly.

## What This Uses

- `alerter --title/--subtitle/--message`
- persistent alert-style notifications
- `--actions "Re-auth now"`
- `--closeLabel Dismiss`
- `--timeout 45`
- `--group` replacement
- `--sound default`
- `--appIcon` and `--contentImage` with a system icon fallback
- returned action text to decide whether to run `oci session authenticate ...`

## Pros

- Richest CLI notification interaction model.
- Can distinguish dismiss, timeout, content click, and named action.
- Does not require Hammerspoon for the click path.
- Persistent alert behavior is harder to miss.

## Cons

- Adds a less common runtime dependency: `brew install vjeantet/tap/alerter`.
- `auth notify` blocks until the alert is dismissed, clicked, or times out.
- Running auth after a returned action adds more process-control code.
- CLI syntax changed in newer alerter versions, so compatibility needs care.

## Code Burden

High for a CLI notifier. The result-handling path is useful, but it is more
stateful than Hammerspoon or terminal-notifier.
