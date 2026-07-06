CREATE TABLE [dbo].[Customer] (
  [CustomerId] INT IDENTITY(1,1) PRIMARY KEY,
  [Rowguid] uniqueidentifier NOT NULL,
  [AccountNumber] nvarchar(10) NOT NULL,
  [IsActive] bit NOT NULL,
  [ModifiedDate] datetime2 NOT NULL
);

CREATE TABLE [dbo].[SalesOrderHeader] (
  [SalesOrderId] INT IDENTITY(1,1) PRIMARY KEY,
  [CustomerId] INT NOT NULL,
  [OrderDate] datetime2 NOT NULL,
  [TotalDue] money NOT NULL,
  CONSTRAINT [FK_SalesOrderHeader_Customer] FOREIGN KEY ([CustomerId]) REFERENCES [dbo].[Customer]([CustomerId])
);

CREATE TABLE [dbo].[SalesOrderDetail] (
  [SalesOrderId] INT NOT NULL,
  [SalesOrderDetailId] INT NOT NULL,
  [UnitPrice] money NOT NULL,
  PRIMARY KEY ([SalesOrderId], [SalesOrderDetailId]),
  CONSTRAINT [FK_SalesOrderDetail_SalesOrderHeader] FOREIGN KEY ([SalesOrderId]) REFERENCES [dbo].[SalesOrderHeader]([SalesOrderId])
);

CREATE INDEX [idx_customer_account] ON [dbo].[Customer] ([AccountNumber]);

-- Focused excerpt in the style of the SQL Server AdventureWorks sample
-- database — trimmed to a few tables that exercise bracket identifiers,
-- schema-qualified names ([dbo].[X]), IDENTITY(1,1), a single-column PK, a
-- composite PK, a schema-qualified FOREIGN KEY ... REFERENCES, an index, and
-- a spread of T-SQL types (nvarchar, bit, datetime2, uniqueidentifier,
-- money). Not the whole schema; assembled for this test only. Kept at
-- end-of-file so it does not shift statement line numbers in the golden
-- snapshot.
--   Reference: https://github.com/microsoft/sql-server-samples (AdventureWorks)
