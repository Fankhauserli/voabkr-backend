# voabkr-backend — Architecture Deepening

This document records an architecture review of the backend (October 2026) and the deepening work agreed from it. It is the companion to `DECK_SHARING_AND_ARCHITECTURE.md`, which specifies deck ownership and sharing; that document's *prose* names for access standings are not the vocabulary — see ADR-0004 and `GLOSSARY.md`.

## Why

The backend reached ~7.8k lines with two seams in it: sqlc's generated `DBTX`, and a hand-written `cache.Cache`. Everything else takes concrete types, so no handler's data path is testable, and rules that exist only as inline conditions get re-spelled, mis-ordered, or omitted. Three defects found during the review are all symptoms of that:

- `handlers/deck.go:187` — the `deck:<id>` cache read returns before the private-deck check at `:221`, under a user-agnostic key, so a body carrying `UserAccessRole` and `ShareToken` was served to anonymous callers.
- `handlers/card.go:116,195,241` — `CreateCards`, `UpdateCard` and `DeleteCard` never read the session: any logged-in learner could write into any Deck.
- `handlers/deck.go:358,411,606` — `OwnerID.Valid && … != userID` never fires for a System deck, so ownerless Decks could be deleted, shared and revoked by anyone.

## The modules

**`access/`** — answers "may this learner act on this Deck", and resolves their **Access role** for the response. Pure: it takes the Deck, the caller's DeckShare and the caller, and holds the spec's authorization matrix as data. Callers fetch and ask; they never branch on role to decide permission. System decks fail closed for edit, share and delete, and stay readable by `is_public`. Account erasure deletes the Decks a learner owns, so `owner_id IS NULL` afterwards only ever means seeded system content.

**`scheduling/`** — `Next(state, grade)` lifts the SM-2 scheduler out of `UpdateReview` (`handlers/review.go:150-304`), where the deepest behaviour in the repo sat unreachable by any test. Today's algorithm is preserved exactly, including the efactor remap and the rule that an Interval strictly increases. The Grade is guarded at the edge (`min=1,max=4`) and in the contract, so an out-of-range value is rejected instead of silently recorded as Good. The `GetUserCard`-error-becomes-`CreateUserCard` path is fixed in the same pass: only "no row" creates, anything else is a failure.

**Identity** — one step applied to both route groups produces the caller once; private routes additionally gate on it; handlers stop parsing the session. The `user_id`-as-decimal-string convention currently lives at eighteen call sites in four divergent variants. Corrupt sessions answer 401 like every other route, not 400.

**`decks/`, `cards/`, `reviews/`** — one reader per entity owns its read path: key, TTL, fetch on miss, row→wire mapping, and invalidation. Handlers ask for a Deck or a Card and never see a database row, which is what retires the separate row→wire mapping candidate. Shared reads (Deck by id, Cards by Deck) are cached; per-caller reads (the Deck list, the Review queue) are not, and neither is the share lookup that resolves a role.

**`helpers/mail.go` → delivery only** — a rendered message in, a distinct outcome out (delivered, skipped because unconfigured, failed), with the SMTP transport behind an internal seam and configuration read once at construction. Registration succeeds once the row exists; delivery failure is logged and resend is the recovery path, rather than rolling the learner back on an SMTP hiccup. A small token module replaces the three generators at `handlers/deck.go:19`, `helpers/mail.go:33` and `handlers/auth.go:245`.

## Cross-cutting decisions

Recorded as ADRs so they are not re-litigated:

- **ADR-0001** — the cache stores data, never decisions.
- **ADR-0002** — no in-process cache tier in production while invalidation cannot cross replicas.
- **ADR-0003** — database-backed tests run against real Postgres via testcontainers.
- **ADR-0004** — the served Access role strings are the vocabulary.

The wire contract is frozen: no response shape changes as part of this work. `handlers/deck.go:513-521`'s hand-built response and the `GetSharedDeckByToken` hit/miss shape difference are preserved deliberately and noted as follow-ups rather than fixed here.

## Task order

| # | Task | Closes |
| - | ---- | ------ |
| 0 | Strip the `deck:<id>` cache read and write from `GetDeckByID` | the private-deck leak, and `RevokeDeckShare`'s stale role |
| 1 | Land the in-flight pagination work: `limit`/`offset` apply only when sent, and correct `go.mod`'s `// indirect` on Prometheus | silent truncation at 100 Cards for the current frontend |
| 2 | Testcontainers harness: one Postgres per package, migrations once, skip locally without a runtime, fail in CI | the `if h.DB == nil` branches and the nil-handler tests |
| 3 | Identity step + `access/`, wired through every Deck and Card handler | both card write holes, and the System-deck guards |
| 4 | Readers + cache ownership, and `VALKEY_DISABLE_LOCAL_CACHE=true` in the infra chart | key literals in handlers, and cross-replica staleness |
| 5 | `scheduling/` + the `CreateUserCard` error path | a silent "recorded as Good", and duplicate Reviews on a database blip |
| 6 | Mailer made delivery-only + the token module | three token generators, and "never sent" reading as "sent" |

Task 4 touches a second repository: `voyagera-ch/infra`, `charts/vocabkr/values.yaml`.

## Not in scope

Verified as earning their keep, and deliberately left alone: `cache/cache.go`'s tiering (only its unused `error` channel is surplus), `cmd/valkey_store.go` (a deep adapter behind a four-method store), `middleware/metrics.go`, `helpers/password.go`, and the generated `types/*.pb.go` and `sql/db/query.sql.go`. The nine generated-but-unused queries cost nothing and let a schema change land in one file.

Noted, not actioned: the `limit = 1000` magic number in `GetReviews` when `deck_id` is present; the divergence between the cached and uncached shapes of `GetSharedDeckByToken`; and `getDB()` in `cmd/db.go`, which has no callers and re-runs migrations.
