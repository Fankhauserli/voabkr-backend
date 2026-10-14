# The cache stores data, never decisions

A cached entry holds shared data only — a Deck row, a list of Cards — and never anything derived from who is asking. Access role in particular is computed per request from the Deck and the caller's DeckShare, and is not cached, so no entry can carry one learner's standing into another learner's response.

## Considered options

**Per-user cached bodies** (keying a Deck by deck *and* user) were rejected: they multiply entries by users × decks, and they hide a decision about one learner inside an entry named for a shared object, which is how a per-deck key came to serve a per-user body — access role and share token included — to anonymous callers.

## Consequences

Every request pays one indexed share lookup to resolve the caller's role. In exchange, a cached entry is identical for every caller, a revoked DeckShare takes effect on the next request rather than at TTL expiry, and adding a field to a cached body can never widen who may read it.
