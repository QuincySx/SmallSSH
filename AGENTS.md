# Development workflow

- The user works on this Mac concurrently. Development must never bring a window to the foreground automatically. Do not raise windows, launch a regular desktop build for verification, use global key events, or open system dialogs just to inspect the app.
- Use `go run ./tools/dev run` for live development. Every new process starts hidden, with an independent development data directory. Use `go run ./tools/dev status` to inspect readiness, visibility, focus, and activation count without showing a window.
- Run `go run ./tools/dev show` only after an explicit human request to open or show the app. Do not use it merely for testing. `stop` performs graceful shutdown. Restarting after a previously shown process must still start hidden.
- Prefer the headless UI Tester and the opt-in offline form snapshots for layout and flow verification. Do not enter real server credentials into generated QA fixtures.
- Package through `go run ./tools/build -platform ...`. Preserve the pinned vendor tree and the macOS password accessibility patch checked by `patches/vendor-integrity.json`; do not regenerate vendor or bypass it with `-mod=mod`.
