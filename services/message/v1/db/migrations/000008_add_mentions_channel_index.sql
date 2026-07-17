-- GetReadStates resolves unread mention counts through this index, so the
-- join is a nested-loop probe instead of a full scan of message_mentions.
CREATE INDEX IF NOT EXISTS message_mentions_user_channel_msg_idx
    ON message_mentions (user_id, message_id DESC);
