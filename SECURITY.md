# Security policy

## Supported version

Security fixes are provided for the latest published release.

## Report a vulnerability

Use GitHub's private vulnerability reporting for this repository. Do not open a public Issue for an unpatched vulnerability.

Include the affected version, impact, minimal reproduction, and whether the private WSS URL or host was exposed. Remove passwords, cloud credentials, private links, tokens, models, and user data from logs and screenshots.

## Operator responsibilities

- Keep the complete WSS URL private; its path contains the access token.
- Run `katago-remote reset-link` immediately after accidental disclosure.
- Stop the service when it is not in use.
- Keep the server image and NVIDIA driver updated.
- Do not modify the listener to bind a public interface.
- Do not use this single-user tool as a public or paid multi-user service.

The installer verifies pinned SHA-256 digests for every downloaded executable, archive, model, and configuration file. The release binary is built with CGO disabled and can be verified against `SHA256SUMS`.
