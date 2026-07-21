-- This index duplicated message_mentions_user_idx exactly. Drop it for
-- databases that applied the earlier migration and keep fresh databases free
-- of the redundant write and storage overhead.
DROP INDEX IF EXISTS message_mentions_user_channel_msg_idx;
