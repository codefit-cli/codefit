-- STATE (a) — GENUINE BLINDNESS, and the CORRECT kind of over-report.
--
-- ALTER COLUMN ... TYPE is a RECOGNIZED skip: the reducer knows what it is and
-- the neutral model carries no column type width, so what this statement
-- declares really is something codefit did not see. It must keep reporting
-- under the blindness reason. This is the residual the statement census
-- deliberately does not close.
ALTER TABLE _user ALTER COLUMN email TYPE varchar(320);
