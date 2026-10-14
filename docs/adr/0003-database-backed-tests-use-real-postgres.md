# Database-backed tests run against real Postgres

Tests that exercise a handler's data path provision a real Postgres with testcontainers, one container per test package with migrations applied once, rather than a hand-written in-memory stand-in for the query layer. Postgres has a genuine local stand-in, so the database is treated as a local-substitutable dependency: there is no interface at the handler's edge, and tests cross the same seam callers do.

## Considered options

A hand-written fake of the generated queries was rejected: with roughly thirty queries it would be large and would drift from the schema, and the drift would be invisible — the fake would keep passing. Tests skip when no container runtime is reachable, and fail when `CI` is set, so the only coverage of the database paths cannot disappear silently.

Production is unaffected: it connects to the CloudNativePG cluster as before. The test image should be pinned to the same major version production runs.
