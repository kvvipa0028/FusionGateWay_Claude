# WP-06

Implemented hash-only management and short-lived stage capabilities, exact persistent task/run/role/attempt/revision/generation/project/audience validation, expiry and revocation, Origin/query/header rejection, and outer guards across legacy gateway, GUI, SSE/native bridge and Codex WebSocket ingress. Legacy Fusion execution and discovery remain closed pending authenticated replacements.

54 Fusion top-level race tests passed (one parent helper skip); 7 policy tests passed. Gateway/GUI ingress and WebSocket negatives passed with zero provider calls; affected untagged upstream regression passed 1744 tests, 16 skips. Fusion CLI/GUI builds and vet passed.

No real task API, authenticated GUI interaction, Runtime admission/stop or final product test family is claimed.
