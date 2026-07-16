# Current Limitations and Evolution

Redis is currently configured as a single node and is a shared failure point
for Presence, Session discovery, resume ownership, and realtime routing. Redis
failure affects new connections, resume, and Dispatcher delivery. Sentinel,
Cluster, route caches, and broadcast fallback are not implemented.

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
complete operational runbook yet. Redis/Kafka failure scenarios for Gateway,
Session, and Dispatcher still need integration and load testing.
