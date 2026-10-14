# The in-process cache tier is off in production

`TwoLevelCache` keeps a per-process L1 in front of the Valkey cluster, but invalidation on write clears only the process that served the write. With the backend running three replicas, the other two keep serving their own L1 copy until TTL expiry (`VALKEY_LOCAL_TTL_SEC`, 120s by default), so an owner who renames a Deck can refresh and see the old name. Production therefore sets `VALKEY_DISABLE_LOCAL_CACHE=true` and reads from the L2 cluster, one hop away in-cluster.

## Considered options

Keeping L1 with a short TTL was rejected: a TTL is not an invalidation strategy, and "stale for up to two minutes" is not a property any caller can reason about. The tier may be re-enabled once invalidation crosses processes — for example a pub/sub channel each replica subscribes to — at which point this ADR should be superseded rather than quietly reversed.
