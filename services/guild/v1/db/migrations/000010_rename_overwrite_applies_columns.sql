-- Rename permission overwrite subject columns to applies_to / applies_to_id.
-- Primary key and indexes follow renamed columns automatically.

ALTER TABLE guild_channel_permission_overwrites
    RENAME COLUMN target_type TO applies_to;

ALTER TABLE guild_channel_permission_overwrites
    RENAME COLUMN target_id TO applies_to_id;
