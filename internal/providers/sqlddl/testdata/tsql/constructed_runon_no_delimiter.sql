-- AUTHORED fixture (constructed_*, never vendored): four CREATE TABLE
-- statements run together with NO ';' and NO batch separator between them.
-- This is valid T-SQL and is the exact shape that used to lose every statement
-- after the first one SILENTLY — tables=1, the first table still
-- StructureProven, nothing in Schema.Unreduced, an empty completeness note.
--
-- The shape is transcribed from a real public warehouse script (kenapDW's
-- CREATE_DW.sql, 7 declared tables, 1 parsed) whose contents are NOT vendored
-- here; the column sets below are authored.
--
-- The lowercase "go" on line 16 is deliberate: T-SQL's batch separator is
-- case-insensitive in every client that implements it, and only separates
-- "use" from the first CREATE TABLE. It is NOT what separates the four tables
-- from each other — nothing does.
use RUNONDB
go

CREATE TABLE Dim_Price_Table
(Type_SID INTEGER IDENTITY(1,1) NOT NULL PRIMARY KEY,
_Type VARCHAR(1))

CREATE TABLE Dim_Car
(Car_SID INTEGER IDENTITY(1,1) NOT NULL PRIMARY KEY,
License_Plate VARCHAR(7),
Brand VARCHAR(30),
Type_sid INT FOREIGN KEY REFERENCES Dim_Price_Table(Type_SID),
Prod_Year INT
)

CREATE TABLE Dim_Renting_Point(
Point_SID TINYINT NOT NULL PRIMARY KEY,
City VARCHAR(30))

CREATE TABLE Fact_Reservation(
Reservation_SID INT IDENTITY(1,1) NOT NULL PRIMARY KEY,
Car_SID INT FOREIGN KEY REFERENCES Dim_Car(Car_SID),
Point_SID TINYINT FOREIGN KEY REFERENCES Dim_Renting_Point(Point_SID),
Total_Price MONEY)
