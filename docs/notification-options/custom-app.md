# Custom App Backend

This branch adds a dedicated `OCI Access.app` helper and routes
`oci-context auth notify` through that app.

## What This Uses

- `oci-context daemon install notifier-app`
- generated `~/Applications/OCI Access.app`
- Swift `NSUserNotificationCenter` helper
- app-owned title and notification identity
- `Re-auth now` action handled inside the helper app
- direct `oci session authenticate ...` process launch from the app

## Pros

- Cleanest macOS notification identity.
- Notification Center can show `OCI Access` instead of Hammerspoon or a CLI tool.
- No Hammerspoon dependency for the notification/action path.
- The helper can grow into a proper signed/notarized app later.

## Cons

- Highest code and install burden.
- Requires Xcode command line tools/`swiftc` to build locally.
- Needs app packaging, future signing/notarization, and versioning if promoted.
- Uses legacy `NSUserNotificationCenter` in this prototype for small code size.
- More moving parts than Hammerspoon or CLI notifier backends.

## Code Burden

High. This is the right shape for polished user experience, but it turns a CLI
integration into app packaging and lifecycle work.
