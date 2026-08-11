-- STATE (b) — a correct NO-OP.
--
-- Both columns are already declared in V1, and both adds are guarded by
-- IF NOT EXISTS, so the right reduction of every statement in this file is to
-- add nothing. It therefore leaves NO position in the model -- by definition,
-- not by failure. Before the statement census, that absence was read as
-- "codefit read NOTHING from this file", which is the opposite of the truth.
ALTER TABLE _user ADD COLUMN IF NOT EXISTS license_status text;
ALTER TABLE _user ADD COLUMN IF NOT EXISTS license_expires_at timestamptz;
