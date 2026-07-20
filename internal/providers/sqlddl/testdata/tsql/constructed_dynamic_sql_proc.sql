-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any upstream
-- corpus. Hand-written for codefit's own test suite (feat/tsql-routine-fixtures)
-- to supply the DB-030 real-POSITIVE cell T-SQL lacks upstream.
--
-- Why constructed: no procedure in the vendored AdventureWorks objects builds
-- SQL dynamically — uspGetBillOfMaterials (recursive CTE) and
-- uspUpdateEmployeePersonalInfo (a single parameterized UPDATE) are both real
-- DB-030 NEGATIVES, and a grep of the upstream install script for sp_executesql
-- / EXEC(@ finds none in a trigger/proc we vendor. So the POSITIVE fire path is
-- proven here by this small, deliberately hand-written procedure instead. Same
-- honesty discipline as DB-020's positive and DB-031's PG negative: the
-- synthetic origin is declared, never implied upstream.
--
-- The body below is the canonical T-SQL dynamic-SQL antipattern: it builds a
-- query string by concatenating a caller-supplied column name and value, then
-- runs it with sp_executesql — a DB-030 positive. Captured COMPLETE to the GO
-- batch separator (ADR 0027).

CREATE PROCEDURE [dbo].[SearchProducts]
    @ColumnName nvarchar(64),
    @Value nvarchar(64)
AS
BEGIN
    DECLARE @sql nvarchar(max);
    SET @sql = N'SELECT * FROM Production.Product WHERE ' + @ColumnName + N' = ' + @Value;
    EXEC sp_executesql @sql;
END;
GO
