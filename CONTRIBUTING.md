# Contributing

Thank you for helping make remote KataGo easier and safer.

## Before a pull request

1. Open or reference an Issue for behavior changes.
2. Keep the one-command path simple and preserve existing tokens and custom files on upgrades.
3. Never commit real WSS links, credentials, cloud instance IDs, or private logs.
4. Add deterministic tests for protocol, lifecycle, and installer changes.
5. Run `scripts/check-repository.sh`.

Changes to the tunnel, authentication, or WebSocket bridge must also pass `scripts/test-public-tunnel.sh` on a supported machine.

The public protocol bridge must remain bound to localhost, must not execute client-provided shell commands, and must reject unbounded work. Changes to these boundaries require an explicit security rationale in the pull request.

Use LF line endings, `gofmt` for Go files, and ShellCheck-clean Bash. User-facing instructions should be updated in both English and Simplified Chinese.
