# Hammerspoon Backend

This branch documents the current Hammerspoon-backed implementation.

## What This Uses

- `hammerspoon://oci-auth-needed?...` URL event
- `hs.notify` with `actionButtonTitle = "Re-auth now"`
- `hs.notify:setIdImage` through the notification attributes table
- custom title/subtitle/body text
- Hammerspoon callback launches `oci session authenticate ...`
- generated wake hook nudges the daemon and emits actionable auth notifications

## Pros

- Already installed and working locally.
- Best automation ergonomics because Hammerspoon can run arbitrary Lua and tasks.
- Smallest production code burden in `oci-context`.
- Easy to integrate with wake hooks and local desktop automation.

## Cons

- Notification still belongs to Hammerspoon at the macOS app identity layer.
- Custom identity image uses a private Hammerspoon/macOS notification API.
- Requires Hammerspoon running and configured.
- Less clean for other users who do not already use Hammerspoon.

## Code Burden

Low to moderate. Most complexity lives in generated Lua, not Go packaging.
