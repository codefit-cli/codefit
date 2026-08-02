-- Vendored VERBATIM excerpt from Microsoft's real AdventureWorksDW (data
-- WAREHOUSE) install script — a real fact table, two real dimension tables,
-- their real primary keys and the fact's real foreign-key block, COPIED from
-- upstream (not "in the style of"). This is the DW counterpart of the OLTP
-- excerpt already vendored in adventureworks_real_objects.sql, and it exists
-- to dogfood the DW-0xx star-schema rule family (RF-03 OLAP closure, slice S2)
-- against a genuine Kimball warehouse rather than hand-written approximations.
--
--   Source:  https://github.com/microsoft/sql-server-samples
--            samples/databases/adventure-works/data-warehouse-install-script/
--            instawdbdw.sql
--   Objects taken (verbatim, unaltered):
--     - CREATE TABLE [dbo].[DimCustomer]         (surrogate CustomerKey)
--     - CREATE TABLE [dbo].[DimDate]             (the time dimension)
--     - CREATE TABLE [dbo].[FactInternetSales]   (the fact, 8 dimension FKs)
--     - ALTER TABLE ... PRIMARY KEY for each of the three
--     - ALTER TABLE [dbo].[FactInternetSales] ADD ... the whole FK block
--
-- Not the whole install script — trimmed to these objects only. Trimming
-- unrelated objects is fine; the DDL of the objects kept below is NOT altered
-- (byte-for-byte from upstream; the upstream file is already LF-terminated and
-- carries no BOM, so no normalization was needed at all). Foreign keys that
-- reference objects NOT present in this excerpt (DimCurrency, DimProduct,
-- DimPromotion, DimSalesTerritory) are expected and deliberate: they are what
-- gives FactInternetSales its real fan-out, and the DDL parser is structural —
-- it does not resolve cross-object references at parse time.
--
-- WHAT THIS FIXTURE PROVES TODAY: the star is visible AS VENDORED, under
-- Microsoft's own names, with no rename or mutation. This header used to
-- declare the opposite — that PascalCase Kimball naming
-- (FactInternetSales, DimCustomer) went unrecognized and that therefore every
-- table classified "unclassified" with NO DW-0xx rule firing. BOTH limits
-- behind that are now closed: the role-name vocabulary recognizes the
-- separator-free PascalCase spelling, and the reducer reads the
-- ALTER TABLE ... ADD CONSTRAINT shapes this DDL uses, so its 3 primary keys
-- and 8 foreign keys reach the model and corroborate the recognized names.
-- Measured here: 3 tables, 3/3 StructureProven, paradigm olap,
-- FactInternetSales -> fact and DimCustomer/DimDate -> dimension.
-- Test-locked, not prose — see
-- internal/providers/sqlddl/dw_integration_test.go
-- (TestDW_AdventureWorksDW_StarIsVisible_AsVendored).
--
-- A THIRD limit, one layer further down, is CLOSED TOO as of
-- tsql-bracketed-type-names. This header used to read: "its bracketed T-SQL
-- type names ([int], [nvarchar](50)) do not match the type vocabulary, so all
-- 74 of its columns read as db.TypeUnknown. That is why DW-002 fires on
-- DimCustomer and DimDate even though both keys really are single-column
-- integer surrogates." The tokenizer canonicalizes a delimited identifier to
-- "..." and the type lookup now unwraps that form, so all 74 columns classify
-- and the DW-002 false positive is GONE — CustomerKey and DateKey are proven
-- integer surrogates, which is what DW-002's own doc comment always said this
-- shape should be. Measured here: 74 of 74 columns classified, ZERO
-- dw-dimension-no-surrogate-key items. Test-locked, not prose — see
-- TestDW002_AdventureWorksDW_SurrogateKeysAreProven and
-- TestDelimitedTypeName_TSQLBrackets_ResolveOnRealVendoredDDL.
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
--   VERIFIED by fetching that exact URL while vendoring this file (it states
--   "Microsoft SQL Server Sample Code ... MIT License."), not assumed from the
--   sibling OLTP excerpt.
--
-- codefit itself is Apache-2.0; this file is a vendored MIT-licensed excerpt
-- and carries its own notice above, per architecture/tsql-golden-real-ddl.

CREATE TABLE [dbo].[DimCustomer](
	[CustomerKey] [int] IDENTITY(1,1) NOT NULL,
	[GeographyKey] [int] NULL,
	[CustomerAlternateKey] [nvarchar](15) NOT NULL,
	[Title] [nvarchar](8) NULL,
	[FirstName] [nvarchar](50) NULL,
	[MiddleName] [nvarchar](50) NULL,
	[LastName] [nvarchar](50) NULL,
	[NameStyle] [bit] NULL,
	[BirthDate] [date] NULL,
	[MaritalStatus] [nchar](1) NULL,
	[Suffix] [nvarchar](10) NULL,
	[Gender] [nvarchar](1) NULL,
	[EmailAddress] [nvarchar](50) NULL,
	[YearlyIncome] [money] NULL,
	[TotalChildren] [tinyint] NULL,
	[NumberChildrenAtHome] [tinyint] NULL,
	[EnglishEducation] [nvarchar](40) NULL,
	[SpanishEducation] [nvarchar](40) NULL,
	[FrenchEducation] [nvarchar](40) NULL,
	[EnglishOccupation] [nvarchar](100) NULL,
	[SpanishOccupation] [nvarchar](100) NULL,
	[FrenchOccupation] [nvarchar](100) NULL,
	[HouseOwnerFlag] [nchar](1) NULL,
	[NumberCarsOwned] [tinyint] NULL,
	[AddressLine1] [nvarchar](120) NULL,
	[AddressLine2] [nvarchar](120) NULL,
	[Phone] [nvarchar](20) NULL,
	[DateFirstPurchase] [date] NULL,
	[CommuteDistance] [nvarchar](15) NULL
) ON [PRIMARY];
GO

CREATE TABLE [dbo].[DimDate](
	[DateKey] [int] NOT NULL,
	[FullDateAlternateKey] [date] NOT NULL,
	[DayNumberOfWeek] [tinyint] NOT NULL,
	[EnglishDayNameOfWeek] [nvarchar](10) NOT NULL,
	[SpanishDayNameOfWeek] [nvarchar](10) NOT NULL,
	[FrenchDayNameOfWeek] [nvarchar](10) NOT NULL,
	[DayNumberOfMonth] [tinyint] NOT NULL,
	[DayNumberOfYear] [smallint] NOT NULL,
	[WeekNumberOfYear] [tinyint] NOT NULL,
	[EnglishMonthName] [nvarchar](10) NOT NULL,
	[SpanishMonthName] [nvarchar](10) NOT NULL,
	[FrenchMonthName] [nvarchar](10) NOT NULL,
	[MonthNumberOfYear] [tinyint] NOT NULL,
	[CalendarQuarter] [tinyint] NOT NULL,
	[CalendarYear] [smallint] NOT NULL,
	[CalendarSemester] [tinyint] NOT NULL,
	[FiscalQuarter] [tinyint] NOT NULL,
	[FiscalYear] [smallint] NOT NULL,
	[FiscalSemester] [tinyint] NOT NULL
) ON [PRIMARY];
GO

CREATE TABLE [dbo].[FactInternetSales](
	[ProductKey] [int] NOT NULL,
	[OrderDateKey] [int] NOT NULL,
	[DueDateKey] [int] NOT NULL,
	[ShipDateKey] [int] NOT NULL,
	[CustomerKey] [int] NOT NULL,
	[PromotionKey] [int] NOT NULL,
	[CurrencyKey] [int] NOT NULL,
	[SalesTerritoryKey] [int] NOT NULL,
	[SalesOrderNumber] [nvarchar](20) NOT NULL,
	[SalesOrderLineNumber] [tinyint] NOT NULL,
	[RevisionNumber] [tinyint] NOT NULL,
	[OrderQuantity] [smallint] NOT NULL,
	[UnitPrice] [money] NOT NULL,
	[ExtendedAmount] [money] NOT NULL,
	[UnitPriceDiscountPct] [float] NOT NULL,
	[DiscountAmount] [float] NOT NULL,
	[ProductStandardCost] [money] NOT NULL,
	[TotalProductCost] [money] NOT NULL,
	[SalesAmount] [money] NOT NULL,
	[TaxAmt] [money] NOT NULL,
	[Freight] [money] NOT NULL,
	[CarrierTrackingNumber] [nvarchar](25) NULL,
	[CustomerPONumber] [nvarchar](25) NULL,
	[OrderDate] [datetime] NULL,
	[DueDate] [datetime] NULL,
	[ShipDate] [datetime] NULL
) ON [PRIMARY];
GO

ALTER TABLE [dbo].[DimCustomer] WITH CHECK ADD
    CONSTRAINT [PK_DimCustomer_CustomerKey] PRIMARY KEY CLUSTERED
    (
        [CustomerKey]
    )  ON [PRIMARY];
GO

ALTER TABLE [dbo].[DimDate] WITH CHECK ADD
    CONSTRAINT [PK_DimDate_DateKey] PRIMARY KEY CLUSTERED
    (
        [DateKey]
    )  ON [PRIMARY];
GO

ALTER TABLE [dbo].[FactInternetSales] WITH CHECK ADD
    CONSTRAINT [PK_FactInternetSales_SalesOrderNumber_SalesOrderLineNumber] PRIMARY KEY CLUSTERED
    (
        [SalesOrderNumber], [SalesOrderLineNumber]
    )  ON [PRIMARY];
GO

ALTER TABLE [dbo].[FactInternetSales] ADD
    CONSTRAINT [FK_FactInternetSales_DimCurrency] FOREIGN KEY
    (
        [CurrencyKey]
    ) REFERENCES [dbo].[DimCurrency] ([CurrencyKey]),
	 CONSTRAINT [FK_FactInternetSales_DimCustomer] FOREIGN KEY
    (
        [CustomerKey]
    ) REFERENCES [dbo].[DimCustomer] ([CustomerKey]),
	 CONSTRAINT [FK_FactInternetSales_DimDate] FOREIGN KEY
    (
        [OrderDateKey]
    ) REFERENCES [dbo].[DimDate] ([DateKey]),
	 CONSTRAINT [FK_FactInternetSales_DimDate1] FOREIGN KEY
    (
        [DueDateKey]
    ) REFERENCES [dbo].[DimDate] ([DateKey]),
	 CONSTRAINT [FK_FactInternetSales_DimDate2] FOREIGN KEY
    (
        [ShipDateKey]
    ) REFERENCES [dbo].[DimDate] ([DateKey]),
	 CONSTRAINT [FK_FactInternetSales_DimProduct] FOREIGN KEY
    (
        [ProductKey]
    ) REFERENCES [dbo].[DimProduct] ([ProductKey]),
	CONSTRAINT [FK_FactInternetSales_DimPromotion] FOREIGN KEY
    (
        [PromotionKey]
    ) REFERENCES [dbo].[DimPromotion] ([PromotionKey]),
	CONSTRAINT [FK_FactInternetSales_DimSalesTerritory] FOREIGN KEY
    (
        [SalesTerritoryKey]
    ) REFERENCES [dbo].[DimSalesTerritory] ([SalesTerritoryKey]);
GO

