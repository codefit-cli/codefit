-- Vendored VERBATIM excerpt from Microsoft's real AdventureWorks OLTP
-- install script — a real view, a real multi-statement procedure, and a
-- real multi-statement trigger, COPIED from upstream (not "in the style
-- of"). This file exists to prove the T-SQL body-truncation hypothesis
-- (architecture/tsql-body-truncation-limit, obs #1050) against genuine
-- AdventureWorks DDL rather than hand-written approximations — see
-- architecture/tsql-golden-real-ddl (obs #1054), Condition 1.
--
--   Source:  https://github.com/microsoft/sql-server-samples
--            samples/databases/adventure-works/oltp-install-script/instawdb.sql
--   Objects taken (verbatim, unaltered other than CRLF->LF normalization and
--   stripping the file's leading UTF-8 BOM — the BOM sits on the whole-file
--   first line, outside every object's DDL):
--     - CREATE VIEW    [HumanResources].[vEmployee]              (single SELECT)
--     - CREATE PROCEDURE [dbo].[uspGetBillOfMaterials]            (recursive CTE,
--       multiple internal statements)
--     - CREATE TRIGGER [Purchasing].[uPurchaseOrderDetail]        (AFTER UPDATE,
--       multiple internal statements, TRY/CATCH)
--     - CREATE PROCEDURE [HumanResources].[uspUpdateEmployeePersonalInfo]
--       (multi-statement, TRY/CATCH — DB-031 clean negative)
--     - CREATE TRIGGER [HumanResources].[dEmployee]               (INSTEAD OF
--       DELETE, no cross-table cascade, no external call — DB-040/041 negative)
--
-- Not the whole install script — trimmed to these three objects only.
-- Trimming unrelated objects is fine; the DDL of the objects kept below is
-- NOT altered (byte-for-byte from upstream, aside from CRLF->LF normalization
-- to match this repo's LF convention and stripping the leading UTF-8 BOM). Any cross-object reference not
-- present in this excerpt (e.g. [Production].[BillOfMaterials]) is expected
-- and does not affect the DDL parser, which is structural and does not
-- resolve cross-object references at parse time.
--
-- License: MIT (Microsoft SQL Server Sample Code)
--   Copyright (c) Microsoft Corporation. All rights reserved.
--
--   Permission is hereby granted, free of charge, to any person obtaining a
--   copy of this software and associated documentation files (the
--   "Software"), to deal in the Software without restriction, including
--   without limitation the rights to use, copy, modify, merge, publish,
--   distribute, sublicense, and/or sell copies of the Software, and to
--   permit persons to whom the Software is furnished to do so, subject to
--   the following conditions:
--
--   The above copyright notice and this permission notice shall be
--   included in all copies or substantial portions of the Software.
--
--   THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS
--   OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
--   MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
--   IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
--   CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
--   TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
--   SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
--
--   Full license text: https://github.com/microsoft/sql-server-samples/blob/master/license.txt
--
-- codefit itself is Apache-2.0; this file is a vendored MIT-licensed
-- excerpt and carries its own notice above, per architecture/tsql-golden-
-- real-ddl (obs #1054).

CREATE VIEW [HumanResources].[vEmployee]
AS
SELECT
    e.[BusinessEntityID]
    ,p.[Title]
    ,p.[FirstName]
    ,p.[MiddleName]
    ,p.[LastName]
    ,p.[Suffix]
    ,e.[JobTitle] 
    ,pp.[PhoneNumber]
    ,pnt.[Name] AS [PhoneNumberType]
    ,ea.[EmailAddress]
    ,p.[EmailPromotion]
    ,a.[AddressLine1]
    ,a.[AddressLine2]
    ,a.[City]
    ,sp.[Name] AS [StateProvinceName]
    ,a.[PostalCode]
    ,cr.[Name] AS [CountryRegionName]
    ,p.[AdditionalContactInfo]
FROM [HumanResources].[Employee] e
	INNER JOIN [Person].[Person] p
	ON p.[BusinessEntityID] = e.[BusinessEntityID]
    INNER JOIN [Person].[BusinessEntityAddress] bea
    ON bea.[BusinessEntityID] = e.[BusinessEntityID]
    INNER JOIN [Person].[Address] a
    ON a.[AddressID] = bea.[AddressID]
    INNER JOIN [Person].[StateProvince] sp
    ON sp.[StateProvinceID] = a.[StateProvinceID]
    INNER JOIN [Person].[CountryRegion] cr
    ON cr.[CountryRegionCode] = sp.[CountryRegionCode]
    LEFT OUTER JOIN [Person].[PersonPhone] pp
    ON pp.BusinessEntityID = p.[BusinessEntityID]
    LEFT OUTER JOIN [Person].[PhoneNumberType] pnt
    ON pp.[PhoneNumberTypeID] = pnt.[PhoneNumberTypeID]
    LEFT OUTER JOIN [Person].[EmailAddress] ea
    ON p.[BusinessEntityID] = ea.[BusinessEntityID];
GO

CREATE PROCEDURE [dbo].[uspGetBillOfMaterials]
    @StartProductID [int],
    @CheckDate [datetime]
AS
BEGIN
    SET NOCOUNT ON;

    -- Use recursive query to generate a multi-level Bill of Material (i.e. all level 1
    -- components of a level 0 assembly, all level 2 components of a level 1 assembly)
    -- The CheckDate eliminates any components that are no longer used in the product on this date.
    WITH [BOM_cte]([ProductAssemblyID], [ComponentID], [ComponentDesc], [PerAssemblyQty], [StandardCost], [ListPrice], [BOMLevel], [RecursionLevel]) -- CTE name and columns
    AS (
        SELECT b.[ProductAssemblyID], b.[ComponentID], p.[Name], b.[PerAssemblyQty], p.[StandardCost], p.[ListPrice], b.[BOMLevel], 0 -- Get the initial list of components for the bike assembly
        FROM [Production].[BillOfMaterials] b
            INNER JOIN [Production].[Product] p
            ON b.[ComponentID] = p.[ProductID]
        WHERE b.[ProductAssemblyID] = @StartProductID
            AND @CheckDate >= b.[StartDate]
            AND @CheckDate <= ISNULL(b.[EndDate], @CheckDate)
        UNION ALL
        SELECT b.[ProductAssemblyID], b.[ComponentID], p.[Name], b.[PerAssemblyQty], p.[StandardCost], p.[ListPrice], b.[BOMLevel], [RecursionLevel] + 1 -- Join recursive member to anchor
        FROM [BOM_cte] cte
            INNER JOIN [Production].[BillOfMaterials] b
            ON b.[ProductAssemblyID] = cte.[ComponentID]
            INNER JOIN [Production].[Product] p
            ON b.[ComponentID] = p.[ProductID]
        WHERE @CheckDate >= b.[StartDate]
            AND @CheckDate <= ISNULL(b.[EndDate], @CheckDate)
        )
    -- Outer select from the CTE
    SELECT b.[ProductAssemblyID], b.[ComponentID], b.[ComponentDesc], SUM(b.[PerAssemblyQty]) AS [TotalQuantity] , b.[StandardCost], b.[ListPrice], b.[BOMLevel], b.[RecursionLevel]
    FROM [BOM_cte] b
    GROUP BY b.[ComponentID], b.[ComponentDesc], b.[ProductAssemblyID], b.[BOMLevel], b.[RecursionLevel], b.[StandardCost], b.[ListPrice]
    ORDER BY b.[BOMLevel], b.[ProductAssemblyID], b.[ComponentID]
    OPTION (MAXRECURSION 25)
END;
GO

CREATE TRIGGER [Purchasing].[uPurchaseOrderDetail] ON [Purchasing].[PurchaseOrderDetail]
AFTER UPDATE AS
BEGIN
    DECLARE @Count int;

    SET @Count = @@ROWCOUNT;
    IF @Count = 0
        RETURN;

    SET NOCOUNT ON;

    BEGIN TRY
        IF UPDATE([ProductID]) OR UPDATE([OrderQty]) OR UPDATE([UnitPrice])
        -- Insert record into TransactionHistory
        BEGIN
            INSERT INTO [Production].[TransactionHistory]
                ([ProductID]
                ,[ReferenceOrderID]
                ,[ReferenceOrderLineID]
                ,[TransactionType]
                ,[TransactionDate]
                ,[Quantity]
                ,[ActualCost])
            SELECT
                inserted.[ProductID]
                ,inserted.[PurchaseOrderID]
                ,inserted.[PurchaseOrderDetailID]
                ,'P'
                ,GETDATE()
                ,inserted.[OrderQty]
                ,inserted.[UnitPrice]
            FROM inserted
                INNER JOIN [Purchasing].[PurchaseOrderDetail]
                ON inserted.[PurchaseOrderID] = [Purchasing].[PurchaseOrderDetail].[PurchaseOrderID];

            -- Update SubTotal in PurchaseOrderHeader record. Note that this causes the
            -- PurchaseOrderHeader trigger to fire which will update the RevisionNumber.
            UPDATE [Purchasing].[PurchaseOrderHeader]
            SET [Purchasing].[PurchaseOrderHeader].[SubTotal] =
                (SELECT SUM([Purchasing].[PurchaseOrderDetail].[LineTotal])
                    FROM [Purchasing].[PurchaseOrderDetail]
                    WHERE [Purchasing].[PurchaseOrderHeader].[PurchaseOrderID]
                        = [Purchasing].[PurchaseOrderDetail].[PurchaseOrderID])
            WHERE [Purchasing].[PurchaseOrderHeader].[PurchaseOrderID]
                IN (SELECT inserted.[PurchaseOrderID] FROM inserted);

            UPDATE [Purchasing].[PurchaseOrderDetail]
            SET [Purchasing].[PurchaseOrderDetail].[ModifiedDate] = GETDATE()
            FROM inserted
            WHERE inserted.[PurchaseOrderID] = [Purchasing].[PurchaseOrderDetail].[PurchaseOrderID]
                AND inserted.[PurchaseOrderDetailID] = [Purchasing].[PurchaseOrderDetail].[PurchaseOrderDetailID];
        END;
    END TRY
    BEGIN CATCH
        EXECUTE [dbo].[uspPrintError];

        -- Rollback any active or uncommittable transactions before
        -- inserting information in the ErrorLog
        IF @@TRANCOUNT > 0
        BEGIN
            ROLLBACK TRANSACTION;
        END

        EXECUTE [dbo].[uspLogError];
    END CATCH;
END;
GO

-- ---------------------------------------------------------------------------
-- Appended for the DB-030/DB-031/DB-040/DB-041 routine fixture extension
-- (feat/tsql-routine-fixtures): two more real AdventureWorks objects, copied
-- VERBATIM (byte-for-byte, aside from CRLF->LF normalization) from the SAME
-- upstream instawdb.sql as the three objects above, to supply the CLEAN
-- NEGATIVE cells the first three could not:
--
--   Source:  https://github.com/microsoft/sql-server-samples
--            samples/databases/adventure-works/oltp-install-script/instawdb.sql
--   Objects taken (verbatim, unaltered other than CRLF->LF normalization):
--     - CREATE PROCEDURE [HumanResources].[uspUpdateEmployeePersonalInfo]
--       — a single UPDATE wrapped in BEGIN TRY ... BEGIN CATCH, NO dynamic SQL:
--       a DB-030 NEGATIVE and a DB-031 NEGATIVE (real TRY/CATCH exception
--       handling). (Its CATCH does EXECUTE [dbo].[uspLogError] — error logging;
--       DB-041 external-call is a TRIGGER rule and does not classify a proc.)
--     - CREATE TRIGGER [HumanResources].[dEmployee]  — an INSTEAD OF DELETE
--       trigger whose whole body is DECLARE/SET/RAISERROR/ROLLBACK: it writes
--       NO other table (a DB-040 cascade NEGATIVE) and makes NO external call
--       (a DB-041 NEGATIVE). (DB-031 covers procedures/functions only, not
--       triggers, so it does not classify dEmployee.)
--
-- Complement to the first three objects: uspGetBillOfMaterials (no handler =
-- DB-031 POSITIVE, no dynamic SQL = DB-030 NEGATIVE) and uPurchaseOrderDetail
-- (writes TransactionHistory/PurchaseOrderHeader = DB-040 cascade POSITIVE; its
-- CATCH does EXECUTE [dbo].[uspPrintError]/[uspLogError], which are INTERNAL
-- logging procs — NOT external-effecting — so under DB-041's STRICT vocabulary
-- this trigger is the DB-041 real NEGATIVE / trap, never a positive; the DB-041
-- positive is the constructed xp_cmdshell trigger in
-- constructed_external_call_trigger.sql). Kept at end-of-file so it does
-- not shift the line numbers the existing tests pin for the objects above.
-- Cross-object references ([HumanResources].[Employee], [dbo].[uspLogError])
-- not present in this excerpt are expected and do not affect the structural
-- DDL parser.
-- ---------------------------------------------------------------------------

CREATE PROCEDURE [HumanResources].[uspUpdateEmployeePersonalInfo]
    @BusinessEntityID [int],
    @NationalIDNumber [nvarchar](15),
    @BirthDate [datetime],
    @MaritalStatus [nchar](1),
    @Gender [nchar](1)
WITH EXECUTE AS CALLER
AS
BEGIN
    SET NOCOUNT ON;

    BEGIN TRY
        UPDATE [HumanResources].[Employee]
        SET [NationalIDNumber] = @NationalIDNumber
            ,[BirthDate] = @BirthDate
            ,[MaritalStatus] = @MaritalStatus
            ,[Gender] = @Gender
        WHERE [BusinessEntityID] = @BusinessEntityID;
    END TRY
    BEGIN CATCH
        EXECUTE [dbo].[uspLogError];
    END CATCH;
END;
GO

CREATE TRIGGER [HumanResources].[dEmployee] ON [HumanResources].[Employee]
INSTEAD OF DELETE NOT FOR REPLICATION AS
BEGIN
    DECLARE @Count int;

    SET @Count = @@ROWCOUNT;
    IF @Count = 0
        RETURN;

    SET NOCOUNT ON;

    BEGIN
        RAISERROR
            (N'Employees cannot be deleted. They can only be marked as not current.', -- Message
            10, -- Severity.
            1); -- State.

        -- Rollback any active or uncommittable transactions
        IF @@TRANCOUNT > 0
        BEGIN
            ROLLBACK TRANSACTION;
        END
    END;
END;
GO
