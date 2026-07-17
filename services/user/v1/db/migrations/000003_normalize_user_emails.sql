-- Normalize stored emails to lowercase. Mailbox local parts are treated as
-- case-insensitive by every mainstream provider, and all service entry
-- points lowercase addresses from this migration on. If two active accounts
-- collide after lowercasing, this statement fails the unique index and the
-- conflict must be resolved manually before retrying.
UPDATE users
SET email = lower(email)
WHERE email <> lower(email);

-- Backstop so a future code path cannot reintroduce mixed-case rows.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_email_lowercase_check'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_email_lowercase_check CHECK (email = lower(email));
    END IF;
END $$;
