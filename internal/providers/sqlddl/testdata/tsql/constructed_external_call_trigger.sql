-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any upstream
-- corpus. Hand-written for codefit's own test suite (feat/tsql-routine-fixtures)
-- to supply the DB-041 real-POSITIVE cell T-SQL lacks upstream.
--
-- Why constructed: NO real AdventureWorks trigger makes an external-effecting
-- call. The one vendored trigger that "calls" anything, uPurchaseOrderDetail,
-- only EXECUTEs the INTERNAL logging procs uspPrintError / uspLogError — a
-- normal internal routine call, which under DB-041's STRICT vocabulary is NOT
-- an external call (it is that rule's real NEGATIVE / trap, see
-- adventureworks_real_objects.sql). A grep of the whole upstream install script
-- finds zero xp_cmdshell / sp_OA* / sp_send_dbmail / OPENROWSET, so the POSITIVE
-- fire path — a trigger that reaches OUTSIDE the database → DB-041 must fire — is
-- proven here by this small, deliberately hand-written trigger instead. Same
-- honesty discipline as DB-020's positive, DB-031's PG negative, and DB-040's
-- constructed cases: the synthetic origin is declared in the open, never implied
-- to be upstream.
--
-- The body below is a T-SQL trigger that shells out with xp_cmdshell — the
-- canonical external-effecting call. It is captured COMPLETE to the GO batch
-- separator (ADR 0027), so DB-041 reads the whole body.

CREATE TRIGGER [dbo].[trg_orders_shell] ON [dbo].[Orders]
AFTER INSERT AS
BEGIN
    DECLARE @cmd varchar(200);
    SET @cmd = 'echo order inserted >> C:\audit\orders.log';
    EXEC xp_cmdshell @cmd;
END;
GO
