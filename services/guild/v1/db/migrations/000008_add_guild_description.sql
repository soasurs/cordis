ALTER TABLE guilds
    ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT ''
        CHECK (char_length(description) <= 1024);
