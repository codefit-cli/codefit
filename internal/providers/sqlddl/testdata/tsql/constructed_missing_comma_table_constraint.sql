-- AUTHORED fixture (constructed_*, never vendored): a CREATE TABLE body whose
-- LAST column definition is missing its separating comma before a table-level
-- PRIMARY KEY.
--
-- The shape is transcribed from a real public warehouse script (kenapDW's
-- CREATE_DW.sql, whose Fact_Reservation declares a six-column PRIMARY KEY on
-- the line after "Profit INT" with no comma between them) whose contents are
-- NOT vendored here; the column sets below are authored.
--
-- Before the missing-comma boundary rule this reduced to PrimaryKey=[Profit] —
-- a FABRICATION of the ADR 0034 §2.6 class: a single-column key the DDL never
-- declares, on a table that still reported StructureProven()==true. The defect
-- is delimiter-independent, so this fixture uses ordinary ';' terminators: the
-- run-on separation of ADR 0041 is not involved.
--
-- Dim_Car exercises the T-SQL INLINE foreign-key form (FOREIGN KEY REFERENCES
-- t(c), legal as a COLUMN constraint in T-SQL only) that the boundary rule must
-- never cut, and Dim_Date the legal inline PRIMARY KEY.
CREATE TABLE Dim_Car(
Car_SID INTEGER IDENTITY(1,1) NOT NULL PRIMARY KEY,
License_Plate VARCHAR(7),
Brand VARCHAR(30)
);

CREATE TABLE Dim_Date(
Date_SID INTEGER IDENTITY(1,1) NOT NULL PRIMARY KEY,
_Date DATE
);

CREATE TABLE Dim_Customer(
Customer_SID INTEGER IDENTITY(1,1) NOT NULL PRIMARY KEY,
Surname VARCHAR(30)
);

CREATE TABLE Fact_Reservation(
Car_sid INT FOREIGN KEY REFERENCES Dim_Car(Car_SID),
Date_from INT FOREIGN KEY REFERENCES Dim_Date(Date_SID),
Date_to INT FOREIGN KEY REFERENCES Dim_Date(Date_SID),
Customer_sid INT FOREIGN KEY REFERENCES Dim_Customer(Customer_SID),
Time_of_reservation INT,
Profit INT
PRIMARY KEY(Car_sid, Date_from, Date_to, Customer_sid)
);
