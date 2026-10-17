# The cache stores data, never decisions

A cached entry holds data — a Deck row, a list of Cards, a learner's profile — and never a decision. A decision is a judgment about who may do what: an Access role, or a refusal. Decisions are computed per request and never cached, in any entry, so no entry can carry one learner's standing into another learner's response.

Per-learner data is a different shape and is allowed. When the learner is part of the key, the entry describes exactly one caller and holds only that learner's own fields; a learner's profile cached under their own key is this shape. The forbidden case remains a decision — above all an Access role — because a decision has inputs beyond the learner (the Deck's owner, visibility and DeckShares), so caching it would go stale whenever any of them changes.

## Considered options

**Per-learner cached Deck bodies** (keying a Deck by deck *and* learner) were rejected: they multiply entries by learners × decks, and they hide a decision about one learner inside an entry named for a shared object, which is how a per-deck key came to serve a per-learner body — access role and share token included — to anonymous callers. A profile entry keyed by its learner is the opposite shape: it carries no standing on anything shared.

## Consequences

Every request pays one indexed share lookup to resolve the caller's role. In exchange, a shared entry is identical for every caller, a revoked DeckShare takes effect on the next request rather than at TTL expiry, and adding a field to a cached body can never widen who may read it.

Allowed is not required: a per-learner entry is cached only when the module that owns it can invalidate it on every write. A learner's profile entry is the case this records; the Deck list and the Review queue stay uncached because their inputs change across many shared rows.
