-- CONSTRUCTED (SYNTHETIC) T-SQL fixture for the table-shaped-head floor
-- (ADR 0043). T-SQL has no TEMPORARY keyword: it marks a temporary table by a
-- '#' NAME PREFIX ('#' local to the session, '##' global across sessions), so
-- the keyword widening that admits PostgreSQL's and MySQL's spellings does
-- nothing here — this is a separate recognition, gated on a per-dialect datum
-- (Dialect.HashPrefixedTempTables) so that a PostgreSQL table legitimately
-- named "#weird" is never read as temporary.
--
-- The shape is NOT invented: dw-gravity (github.com/3amory99/Gravity-Books-
-- Sales-End-to-End-Project, DWH Scripts/1.1_CreateDimDate.sql:257) declares
-- 'CREATE TABLE #tmpHoliday(...)' at the top level of a batch script, and that
-- statement matched no dispatch branch at all before this slice. It is
-- reproduced here rather than vendored because the corpus is external.
CREATE TABLE dbo.Keeper (
    Id int NOT NULL PRIMARY KEY,
    Label nvarchar(50) NULL
);
GO
CREATE TABLE #tmpHoliday (DateId int, HolidayText nvarchar(50));
GO
CREATE TABLE ##GlobalScratch (Id int);
GO
