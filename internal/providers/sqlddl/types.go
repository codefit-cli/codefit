package sqlddl

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
)

// postgresTypeMap is the PostgreSQL base-type -> neutral db.Type vocabulary
// used to build Postgres().TypeMap. An unmapped keyword is TypeUnknown
// (honest) — the declared subset grows by dogfooded demand, not speculation.
func postgresTypeMap() map[string]db.Type {
	return map[string]db.Type{
		"bigserial": db.TypeInt, "serial": db.TypeInt, "smallserial": db.TypeInt,
		"bigint": db.TypeInt, "integer": db.TypeInt, "int": db.TypeInt,
		"int2": db.TypeInt, "int4": db.TypeInt, "int8": db.TypeInt, "smallint": db.TypeInt,
		"boolean": db.TypeBool, "bool": db.TypeBool,
		"timestamp": db.TypeDateTime, "timestamptz": db.TypeDateTime,
		"timestamp with time zone": db.TypeDateTime, "timestamp without time zone": db.TypeDateTime,
		"date": db.TypeDateTime, "time": db.TypeDateTime, "timetz": db.TypeDateTime,
		"numeric": db.TypeFloat, "decimal": db.TypeFloat, "real": db.TypeFloat,
		"double precision": db.TypeFloat, "double": db.TypeFloat, "float": db.TypeFloat,
		"float4": db.TypeFloat, "float8": db.TypeFloat, "money": db.TypeFloat,
		"json": db.TypeJSON, "jsonb": db.TypeJSON,
		"bytea":   db.TypeBytes,
		"uuid":    db.TypeString,
		"text":    db.TypeText,
		"varchar": db.TypeString, "character varying": db.TypeString, "char": db.TypeString,
		"character": db.TypeString, "bpchar": db.TypeString, "citext": db.TypeString, "name": db.TypeString,
	}
}

// postgresModifiers is the PostgreSQL column-tail parse-and-ignore vocabulary
// used to build Postgres().Modifiers — the keywords that end a column's type
// expression in splitTypeAndMods.
func postgresModifiers() map[string]bool {
	return map[string]bool{
		"NOT": true, "NULL": true, "DEFAULT": true, "PRIMARY": true, "UNIQUE": true,
		"REFERENCES": true, "CHECK": true, "GENERATED": true, "COLLATE": true, "CONSTRAINT": true,
	}
}

// mysqlTypeMap is the MySQL base-type -> neutral db.Type vocabulary used to
// build MySQL().TypeMap. An unmapped keyword is TypeUnknown (honest) — the
// declared subset grows by dogfooded demand, not speculation (design §3).
func mysqlTypeMap() map[string]db.Type {
	return map[string]db.Type{
		"tinyint": db.TypeInt, "smallint": db.TypeInt, "mediumint": db.TypeInt,
		"int": db.TypeInt, "integer": db.TypeInt, "bigint": db.TypeInt,
		"decimal": db.TypeFloat, "numeric": db.TypeFloat, "float": db.TypeFloat,
		"double": db.TypeFloat, "real": db.TypeFloat,
		"date": db.TypeDateTime, "datetime": db.TypeDateTime, "timestamp": db.TypeDateTime,
		"time": db.TypeDateTime, "year": db.TypeDateTime,
		"char": db.TypeString, "varchar": db.TypeString, "binary": db.TypeString,
		"tinytext": db.TypeText, "text": db.TypeText, "mediumtext": db.TypeText, "longtext": db.TypeText,
		"blob": db.TypeBytes, "tinyblob": db.TypeBytes, "mediumblob": db.TypeBytes,
		"longblob": db.TypeBytes, "varbinary": db.TypeBytes,
		// MySQL SET is a multi-value bitmask, not a single-value enum, but the
		// neutral db model has no bitmask concept; both fold onto TypeEnum. The
		// full SET(...) / ENUM(...) literal is preserved verbatim in RawType, so
		// no raw signal is lost — a future rule needing SET-vs-ENUM reads RawType.
		"enum": db.TypeEnum, "set": db.TypeEnum,
		"json":    db.TypeJSON,
		"boolean": db.TypeBool, "bool": db.TypeBool,
	}
}

// mysqlModifiers is the MySQL column-tail parse-and-ignore vocabulary used to
// build MySQL().Modifiers — the keywords that end a column's type expression
// in splitTypeAndMods, including MySQL-specific tail keywords (UNSIGNED,
// ZEROFILL, AUTO_INCREMENT, CHARACTER SET, COLLATE) alongside the universal
// ones shared with PostgreSQL.
func mysqlModifiers() map[string]bool {
	return map[string]bool{
		"NOT": true, "NULL": true, "DEFAULT": true, "PRIMARY": true, "UNIQUE": true,
		"REFERENCES": true, "CHECK": true, "CONSTRAINT": true, "COMMENT": true,
		"UNSIGNED": true, "ZEROFILL": true, "AUTO_INCREMENT": true,
		"CHARACTER": true, "COLLATE": true, "ON": true,
	}
}

// sqlserverTypeMap is the T-SQL base-type -> neutral db.Type vocabulary used
// to build SQLServer().TypeMap. An unmapped keyword is TypeUnknown (honest) —
// the declared subset grows by dogfooded demand, not speculation (design §3).
// Every entry maps onto an EXISTING db.Type enum value; no core enrichment.
func sqlserverTypeMap() map[string]db.Type {
	return map[string]db.Type{
		"int": db.TypeInt, "bigint": db.TypeInt, "smallint": db.TypeInt, "tinyint": db.TypeInt,
		"nvarchar": db.TypeString, "nchar": db.TypeString, "varchar": db.TypeString, "char": db.TypeString,
		"uniqueidentifier": db.TypeString,
		"bit":              db.TypeBool,
		"datetime2":        db.TypeDateTime, "smalldatetime": db.TypeDateTime,
		"datetime": db.TypeDateTime, "date": db.TypeDateTime,
		"money": db.TypeFloat, "smallmoney": db.TypeFloat,
		"decimal": db.TypeFloat, "numeric": db.TypeFloat, "float": db.TypeFloat, "real": db.TypeFloat,
		"varbinary": db.TypeBytes, "binary": db.TypeBytes, "image": db.TypeBytes,
	}
}

// sqlserverModifiers is the T-SQL column-tail parse-and-ignore vocabulary used
// to build SQLServer().Modifiers — the keywords that end a column's type
// expression in splitTypeAndMods, including T-SQL-specific tail keywords
// (IDENTITY, ROWGUIDCOL) alongside the universal ones shared with
// PostgreSQL/MySQL.
func sqlserverModifiers() map[string]bool {
	return map[string]bool{
		"NOT": true, "NULL": true, "DEFAULT": true, "PRIMARY": true, "UNIQUE": true,
		"REFERENCES": true, "CHECK": true, "CONSTRAINT": true,
		"IDENTITY": true, "ROWGUIDCOL": true, "COLLATE": true,
	}
}

// mapType maps a base type name (already stripped of length/precision and
// array markers by typeBase) to the neutral db.Type via this dialect's
// TypeMap. An unmapped keyword is the honest db.TypeUnknown fallback.
func (d *Dialect) mapType(base string) db.Type {
	if t, ok := d.TypeMap[strings.ToLower(strings.TrimSpace(base))]; ok {
		return t
	}
	return db.TypeUnknown
}

// typeBase strips a type's length/precision "(...)" and array "[]" markers, leaving
// the bare type name for mapping (VARCHAR(30) -> varchar, tag[] -> tag).
func typeBase(raw string) string {
	b := strings.TrimSpace(raw)
	if i := strings.IndexByte(b, '('); i >= 0 {
		b = b[:i]
	}
	b = strings.TrimSuffix(strings.TrimSpace(b), "[]")
	return strings.TrimSpace(b)
}
