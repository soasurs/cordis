# Server-side Mention Parsing and Expansion

> Status: implemented. This document describes the current repository
> behavior; the Chinese version is [mention-expansion.md](../zh-CN/mention-expansion.md).

## 1. Background and Goals

Mentions used to be parsed by the client, which passed `mention_user_ids` to
the Message service for validation and storage:

- no content markup protocol existed;
- only user mentions were supported, not roles or `@everyone`;
- message objects (internal and public protobuf) carried no mention fields,
  only request parameters and events did;
- `UpdateMessage` treated mentions as an independent field, decoupled from
  content.

This refactor aligns with a subset of Discord's behavior:

- the server parses `<@user>`, `<@&role>`, and `@everyone` from content
  (`@here`, channel mentions, and other markup are not supported);
- message objects and realtime events carry structured mentions;
- `@role`/`@everyone` targets are expanded per channel-visible member and
  materialized per user, so unread `mention_count` includes expanded mentions;
- editing a message rebuilds mentions from the new content instead of
  allowing independent edits;
- large guilds use an asynchronous expansion worker so message creation is
  never blocked by member expansion.

Non-goals (separate future work): `allowed_mentions`, role `mentionable`
toggles, `@here`, `<#channel>`, and per-user suppression settings.

## 2. Content Protocol

### 2.1 Markup

| Type | Format | Notes |
|---|---|---|
| User | `<@USER_ID>` | The deprecated `<@!USER_ID>` form is normalized to the same user mention |
| Role | `<@&ROLE_ID>` | Guild text channels only |
| everyone | `@everyone` | Complete word match, case-sensitive; `@Everyone` is not parsed |

### 2.2 Parsing Rules

- Mentions may appear anywhere in content; IDs must be positive decimal
  integers, and overflow or malformed markup stays as plain text;
- backslash escaping: `\<@123>` and `\@everyone` are not parsed (consecutive
  backslashes follow regular escaping rules);
- `@everyone` requires word boundaries: the character before and after may
  not be a letter, digit, or underscore;
- results are deduplicated and returned in ascending user/role ID order
  (storage, responses, and events do not preserve content order);
- limits: user plus role mentions must not exceed `mentionsPerMessage`
  (default 100); `@everyone` is not counted;
- DM channels only parse `<@>`; `<@&>` and `@everyone` remain plain text and
  are neither permission-checked nor stored;
- invalid entities (missing users/roles) are dropped from the result without
  an error, mirroring Discord.

## 3. Permissions

### 3.1 New Permission Bit

`proto/guild/v1/guild.proto` adds:

```proto
GUILD_PERMISSION_MENTION_EVERYONE = 2048;
```

It is included in `AllGuildPermissions` and `AllChannelPermissions` in
`services/guild/v1/internal/server/permissions.go`, so roles and channel
overwrites can grant or set it. `MENTION_EVERYONE` is a channel-level
permission, matching Discord.

### 3.2 Write-time Validation

When a Guild text channel parses `@everyone`, the effective permission set
returned by the existing `AuthorizeGuildChannel` call is checked for
`MENTION_EVERYONE`; a missing permission rejects the whole request.

- role mentions: `ListGuildRoles(guild_id, author_id)` returns every role of
  the Guild in one call, and roles outside the Guild or missing are dropped;
  `mentionable` is not checked in this version;
- user mentions: `BatchGetUserProfiles` filters out users that no longer
  exist;
- the author is already guaranteed to be a Guild member by the existing
  send-message authorization flow.

## 4. Storage

### 4.1 Migration

`services/message/v1/db/migrations/000011_add_mention_expansion.sql`:

```sql
ALTER TABLE message_mentions
    ADD COLUMN source SMALLINT NOT NULL DEFAULT 1
    CHECK (source IN (1, 2));

CREATE TABLE IF NOT EXISTS message_role_mentions (
    message_id  BIGINT NOT NULL,
    role_id     BIGINT NOT NULL CHECK (role_id > 0),
    PRIMARY KEY (message_id, role_id)
);

ALTER TABLE messages
    ADD COLUMN mention_everyone BOOLEAN NOT NULL DEFAULT FALSE;
```

`message_mentions.source` semantics:

- `1`: direct `<@user>` mentions, written synchronously in the message
  transaction;
- `2`: expanded `@role`/`@everyone` rows, written asynchronously by the
  worker.

The `(message_id, user_id)` primary key is unchanged: one row per user per
message regardless of how many mention sources matched, so the existing
unread-count SQL needs no modification.

### 4.2 Model and Store Interface

- `model.Message` gains `Mentions model.MessageMentions` with
  `UserIDs []int64`, `RoleIDs []int64`, and `Everyone bool`;
- Store additions:
  - `ReplaceMessageMentions(ctx, messageID, mentions)` replaces users
    (source 1), roles, and `mention_everyone` in one call;
  - `ListMessageMentions(ctx, messageID)` reads the full mention set;
  - `ListMessagesMentions(ctx, messageIDs)` batch-loads mentions for
    `ListMessages` without N+1 queries;
  - `RebuildExpandedMessageMentions(ctx, messageID, expectedRevision,
    userIDs)` atomically replaces source 2 rows in one transaction, but only
    while the stored revision still matches `expectedRevision`.

## 5. Message Service Flow

### 5.1 CreateMessage

1. existing validation (channel, author, content, attachments, flags, reply
   references);
2. parse content into a mention set;
3. DM channels keep only user mentions;
4. Guild channels: `@everyone` permission check, role existence filter, user
   existence filter;
5. limit check (users + roles);
6. idempotency fingerprint v2 (section 10);
7. transaction: create message, `ReplaceMessageMentions` (source 1 + roles +
   everyone), author `AckMessage`;
8. after commit: publish the message event; the expansion worker consumes it
   and materializes `@role`/`@everyone` targets (section 7).

### 5.2 UpdateMessage

- the request `MentionList` field is removed; mentions are fully derived from
  content:
  - with `HasContent()`: parse the new content, read the old full mention set
    (including already-expanded source 2 rows) as `previous` inside the
    transaction, replace users/roles/everyone, then publish
    `message.updated` (with `previous` and the current set) after commit; the
    worker rebuilds expanded rows from that event's revision;
  - without content changes: mentions and expanded rows are unchanged and no
    expansion is triggered.
- the `previous` set is the best-effort state observable at transaction
  start; if async expansion is still in flight it may be incomplete. The
  event's current mention set is authoritative; `previous` exists only to
  clean up client-side highlights.

### 5.3 DeleteMessage

- the transaction deletes the message and clears
  `message_mentions`/`message_role_mentions`;
- the delete event carries the best-effort full mention set;
- in-flight expansion events are dropped by the revision/deletion check
  (section 7).

### 5.4 GetMessage / ListMessages

- `GetMessage` loads the full mention set for one message;
- `ListMessages` batch-loads mentions via `ListMessagesMentions`;
- both internal and public `Message` protos expose the new fields
  (section 11).

## 6. Channel-Visible Member Expansion (Guild Batch API)

### 6.1 New RPC

```proto
rpc ListGuildMentionTargets(ListGuildMentionTargetsRequest)
    returns (ListGuildMentionTargetsResponse);

message ListGuildMentionTargetsRequest {
  int64 guild_id = 1;
  int64 actor_user_id = 2;
  int64 channel_id = 3;
  repeated int64 role_ids = 4;
  bool everyone = 5;
  string cursor = 6;
  int32 limit = 7;
}

message ListGuildMentionTargetsResponse {
  repeated int64 user_ids = 1;
  string next_cursor = 2;
}
```

### 6.2 Semantics

- returns active members that can view the channel, ordered by user ID
  ascending; limit defaults to 100 and maxes at 1000;
- `everyone=true`: every channel-visible member;
- non-empty `role_ids`: members holding at least one of the roles and able to
  view the channel; combined with `everyone` the result is the union;
- results are deduplicated;
- the caller is the Message service and passes the message author as
  `actor_user_id` (guaranteed to be a Guild member);
- the endpoint validates that the channel and roles belong to the Guild.

### 6.3 Visibility Rules

Visibility matches the existing `channelPermissions` logic exactly:

1. Guild owner and `ADMINISTRATOR` are always visible;
2. base permissions are the union of the default role and assigned roles;
3. overwrites apply in fixed precedence: the `@everyone` role overwrite
   first, then all assigned role allows/denies aggregated (order-independent),
   then the member overwrite;
4. the final effective permissions must include `VIEW_CHANNEL`;
5. only active members (`deleted_at = 0`) participate.

Implementation: the Guild store pages candidate member/role-target IDs by
user ID, and the server reuses `memberAuthorityFromRoles` and
`channelPermissions` per window, so batch results share the single-user
authorization rules. An integration test compares the batch result against
per-user authorization for the same members.

Pagination: each call scans one candidate window (`limit + 1` candidates,
returning at most `limit` visible members); the cursor always advances to the
last candidate user ID. A window with no visible members returns an empty
page and the caller keeps paging until `next_cursor` is absent.

## 7. Asynchronous Expansion

### 7.1 Topic and Consumer Group

No separate task topic: the worker consumes the existing message event topic
`cordis.message.events.v1`. The event payload already carries everything the
worker needs (`message_id`, `channel_id`, `guild_id`, `revision`, plus the new
`mention_role_ids`, `mention_everyone`, and for updates `rebuild_mentions`).

- consumer group: `cordis.message.mentions.v1` (the `cordis.<consumer>.<source>.v1`
  convention; the consumer is the Message service's own mentions expansion);
- `KafkaConfig` gains `MentionsConsumerGroup` (default above); no new topic
  configuration is needed;
- the worker runs as a background goroutine inside the Message service
  (following the Dispatcher's `kgo.NewClient` + manual commit pattern) and
  only starts when Kafka is configured; multiple instances share work via the
  consumer group;
- event keys keep the existing convention (Guild messages keyed by
  `guild_id`), so events for one message stay ordered within a partition.

### 7.2 Worker Processing

1. filter by event type; `message.created` and `message.updated` events whose
   mentions were rebuilt (`rebuild_mentions` true) are handled; events without
   role/everyone mentions are skipped (historical events lack these fields
   and deserialize empty, so they never trigger);
2. load the message (id, channel_id, guild_id, revision, deleted_at); skip
   and commit when the message is missing, deleted, or the revision does not
   match the event (`message.deleted` needs no handling because the delete
   transaction already clears expanded rows);
3. page through `ListGuildMentionTargets` to collect every target;
4. replace source 2 rows through
   `RebuildExpandedMessageMentions(message_id, event revision, targets)`;
   the store locks the message row, re-checks the revision, deletes the old
   expanded rows, and re-inserts the new set in 10,000-row batches inside one
   transaction, so an edit or delete racing with the expansion cannot leave
   stale rows behind;
5. commit only after success; failures retry with exponential backoff
   (100 ms initial, 5 s cap, at most 8 attempts), then log an alert and
   commit past the record.

### 7.3 Consistency Window

The message event is published before expansion finishes: clients see the
message first, and the server-side `mention_count` becomes ready shortly
after (typically milliseconds to seconds). READY / GetReadStates / AckMessage
return counts based on the materialized rows, so they are eventually
consistent. Real-time highlighting is a client concern: guild-level message
events are broadcast and the client already knows its own roles; the server
does not fan out expansion results per user.

Event publication is best-effort (existing semantics): a lost event also
loses its expansion, and the missing `mention_count` has the same origin as
the client-side event loss. Idempotent retries publish events only when the
message is actually created/updated, so expansion is not duplicated; consumer
group replays skip historical events and safely re-run new ones thanks to
`ON CONFLICT DO NOTHING` and revision checks.

## 8. mention_count Semantics

- count = distinct user rows among unread messages for "direct user mentions
  ∪ expanded mentions";
- the `(message_id, user_id)` primary key means one message counts at most
  once per user; the existing `listReadyChannelReadStatesQuery` CTE is
  unchanged;
- `AckMessage` recomputes counts after advancing the watermark;
- edits delete source 2 rows and rebuild them asynchronously; deletes clear
  rows with the message;
- role membership changes never rewrite history (mentions snapshot the
  members at message time, matching Discord).

## 9. Events

`messagePayload` adds:

```go
MentionRoleIDs          []string `json:"mention_role_ids"`
MentionEveryone         bool     `json:"mention_everyone"`
RebuildMentions         bool     `json:"rebuild_mentions,omitempty"`
PreviousMentionRoleIDs  []string `json:"previous_mention_role_ids,omitempty"`
PreviousMentionEveryone *bool    `json:"previous_mention_everyone,omitempty"`
```

`MentionUserIDs`/`PreviousMentionUserIDs` are retained. The deleted-event
payload gains the same role/everyone fields. `RebuildMentions` is set only on
`message.updated` events whose content changed (mentions were rebuilt);
flags/attachment-only updates do not trigger expansion. Routing is unchanged:
Guild messages publish one guild-keyed record, DM messages one record per
user.

## 10. Idempotency

- fingerprint version is bumped to 2 and includes the parsed user IDs, role
  IDs, and `everyone` flag in addition to the existing fields;
- the fingerprint must contain the parse result rather than only content:
  parsing depends on external state such as user/role existence, and a retry
  may observe different state; only an explicit parse result preserves
  "same fingerprint, same result";
- expansion materialization is not fingerprinted (async and replayable);
- idempotent retries publish events only in the `createdNewMessage` branch,
  so replays never re-trigger expansion.

## 11. proto / API Changes

### 11.1 Internal `proto/message/v1/message.proto`

- `CreateMessageRequest`: `mention_user_ids` removed (field 9 `reserved`);
- `UpdateMessageRequest`: `mentions` removed (field 6 `reserved`); the
  `MentionList` message is deleted;
- `Message` adds `mention_user_ids = 15`, `mention_role_ids = 16`,
  `mention_everyone = 17`.

### 11.2 Public `proto/api/v1/message.proto`

- `CreateMessageRequest`: `mention_user_ids` removed (field 8 `reserved`);
- `UpdateMessageRequest`: `mentions` removed (field 5 `reserved`);
  `MentionList` deleted;
- `Message` adds the three mention fields;
- the API adapter forwards them in `messageToAPI`.

Responses use ID lists rather than Discord's user object arrays (clients
already cache profiles; object expansion is future work). This is a breaking
protocol change; clients must upgrade in lockstep. Deprecated compatibility
fields are intentionally not kept to avoid a dual-source `mention_user_ids`
that would be silently ignored.

## 12. Migration and Compatibility

- existing `message_mentions` rows are kept and treated as `source=1`;
- historical content has no markup: queries return stored values and do not
  backfill or rewrite content;
- `mention_everyone` defaults to false and `message_role_mentions` is empty,
  so old messages produce no role/everyone mentions;
- docs note that historical messages may have mentions inconsistent with
  their content; this is a known legacy artifact.

## 13. Performance and Capacity

- sync path: only direct user mentions (≤100 rows) plus role/everyone
  definitions are written; latency is on par with the previous behavior;
- async path: a 100k-member `@everyone` needs about 100 batch RPC pages
  (page size 1000) and 10 ten-thousand-row `unnest` batches, completing in
  the background within seconds;
- `message_mentions` grows by message × visible members for `@everyone`
  messages; queries use the existing `(user_id, message_id DESC)` index and
  each user sees one row per message;
- future optimization: stream the target computation as a SQL cursor on the
  Guild side to reduce transfer and page count.

## 14. Test Plan

- parser unit tests: markup variants, `<@!>` normalization, escaping, word
  boundaries, case sensitivity, malformed/overflow IDs, dedup and order, DM
  trimming, limits;
- permission tests: `@everyone` without `MENTION_EVERYONE` is rejected, role
  and user existence filters;
- store integration tests: source column migration, role/everyone
  read/write, batch loading, expanded-row delete/re-upsert, unread count
  unaffected;
- Guild batch API integration tests: batch result fully compared against
  per-user single authorization;
- worker tests: event type filtering, skip without expansion need, paging,
  batched upsert, stale revision drop, deleted-message skip, retries;
- server tests: Create/Update/Delete/Get/List mention behavior, edit rebuild
  and `previous`, event payloads, fingerprint v2;
- API tests: field forwarding, request field removal;
- docs synchronized across `services.md`, `protocols-and-errors.md`,
  `data-and-events.md` (en + zh-CN).

## 15. Implementation Steps

All steps are complete and land as a PR stack:

1. Guild: `MENTION_EVERYONE` permission bit + `ListGuildMentionTargets` batch
   API (with visibility comparison tests);
2. Message: migration, model/Store, mention parser
   (`services/message/v1/internal/mention`);
3. Message: Create/Update/Delete/Get/List, events, idempotency fingerprint
   v2;
4. Message: expansion worker (consumes `cordis.message.events.v1`, batched
   upsert, retries);
5. proto and API adapter;
6. full test pass and documentation sync.

## 16. Confirmed Decisions

- permission bit value `2048` (previous enum maximum was 1024);
- breaking protocol change removes request fields directly (`reserved`), no
  deprecated compatibility layer;
- the expansion worker consumes the message event topic; no separate task
  topic;
- role `mentionable` validation is deferred.
