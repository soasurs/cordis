# Current Limitations and Evolution

Local examples use single-node Redis and etcd, but the production target is
Redis Cluster plus an etcd cluster. Redis stores Presence, resume ownership,
and aggregate realtime routing; etcd stores leased Session-node discovery.
Loss of availability or quorum affects the corresponding paths. Route caches
and broadcast fallback are not implemented.

Redis keys use per-entity hash tags and there are no cross-key Lua scripts or
transactions, so Redis Cluster can route them by slot. Cross-slot pipelines are
batching only, not atomic; TTLs, generations, and read-time validation converge
partial writes and stale records.

Logical Session state and the 2048-entry replay buffer exist only in memory.
Node failure loses them. Graceful drain asks clients to identify again; live
state migration is not implemented.

Message and Guild both publish to Kafka best-effort after database commit and
have no transactional outbox, leaving a loss window between commit and publish.
Dispatcher has retry and manual offset commits but no dead-letter queue, global
event ID, or generic deduplication. Calls to target Session nodes are
sequential, so one node failure retries the Kafka record.

Known feature gaps include invites, stronger limits and rate limiting, automatic
role/channel reorder behavior, threads, pinned messages, voice media behavior,
and explicit Gateway protocol version negotiation, compression, and sharding.

There are no unified deployment manifests, cross-service readiness policy, or
complete operational runbook yet. Redis, etcd, and Kafka failure scenarios for
Gateway, Session, and Dispatcher still need integration and load testing.
