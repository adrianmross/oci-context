# Terminal Notifier Backend

This branch makes `oci-context auth notify` use `terminal-notifier` directly.

## What This Uses

- `terminal-notifier -title/-subtitle/-message`
- `-group` for replacement/deduplication
- `-sound default`
- `-appIcon` and `-contentImage` with a system icon fallback
- `-execute` to run `oci session authenticate ...` when the notification is activated
- `-open` as a URL fallback

## Pros

- Small code change compared with a custom app.
- Does not require Hammerspoon to handle the click action.
- Good CLI ergonomics and easy Homebrew install.
- Supports icon/content image knobs.

## Cons

- Adds a runtime dependency on `terminal-notifier`.
- The notification still comes from the terminal-notifier app/binary, not a first-class `OCI Access.app`.
- The direct `-execute` command is convenient but less structured than a signed app handling a URL.
- Branding is limited by what terminal-notifier and macOS allow.

## Code Burden

Moderate. Most code is command argument construction plus dependency detection.
