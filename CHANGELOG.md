# Changelog

## [1.0.1] - 2026-10-13

### Security

- Fix IP allowlist bypass through a caller-supplied `X-Real-IP` header. Authorization now uses the direct TCP peer address.
- Replace incoming `X-Real-IP` and `X-Forwarded-For` values with the verified peer IP when forwarding requests, and remove the incoming `Forwarded` header.

### Changed

- Validate configured allowlist entries as literal IPv4 or IPv6 addresses and compare equivalent IP representations correctly.
- Reject requests with malformed peer addresses. An empty allowlist still permits all valid peers.
- Behind a proxy or NAT, allowlist rules apply to the intermediary's address; allowing it permits clients connecting through it.

[1.0.1]: https://github.com/ArmaanLeg3nd/voidension/compare/v1.0.0...v1.0.1
