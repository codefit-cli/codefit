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

// mapType maps a base type name (already reduced to a TypeMap lookup key by
// typeLookupKey) to the neutral db.Type via this dialect's TypeMap. An
// unmapped keyword is the honest db.TypeUnknown fallback.
func (d *Dialect) mapType(base string) db.Type {
	if t, ok := d.TypeMap[strings.ToLower(strings.TrimSpace(base))]; ok {
		return t
	}
	return db.TypeUnknown
}

// typeBase strips a type's length/precision "(...)" and array "[]" markers, leaving
// the bare type name (VARCHAR(30) -> varchar, tag[] -> tag).
//
// It deliberately does NOT remove identifier delimiters — see typeLookupKey for
// why that step belongs to the TypeMap lookup and not here. isInlineKeyIndexForm
// (reduce.go) is this function's OTHER caller and asks a different question, one
// for which the delimiters are the discriminator rather than noise.
func typeBase(raw string) string {
	b := strings.TrimSpace(raw)
	if i := strings.IndexByte(b, '('); i >= 0 {
		b = b[:i]
	}
	b = strings.TrimSuffix(strings.TrimSpace(b), "[]")
	return strings.TrimSpace(b)
}

// typeLookupKey renders a COLUMN's declared type expression as the key mapType
// looks up in the dialect's TypeMap: typeBase's length/precision and array
// stripping, plus the removal of the identifier delimiters that WRAP the name.
//
// WHY THE UNWRAP IS DIALECT-FREE. Every dialect this package supports lets a
// type name be written delimited — T-SQL's [int], MySQL's `int`, ANSI's "int" —
// and Microsoft's own generated scripts do it for EVERY column
// ([CustomerKey] [int] IDENTITY(1,1) NOT NULL). split() is the sole owner of
// quoting (design §2, ADR 0022) and re-emits every dialect-quoted identifier
// canonicalized to ANSI "..."; a type name occupies an identifier position, so
// by the time this runs all three spellings have already collapsed onto the one
// canonical form. Unwrapping THAT form therefore closes the gap on all three
// dialects with no per-dialect branch and no new Dialect datum — and a fix
// written as a bracket strip would have been both wrong (the tokenizer emits no
// brackets) and dialect-specific.
//
// WHY IT CANNOT DISTURB PostgreSQL's ARRAY MARKER. The trailing [] is a SUFFIX
// with a different meaning in the one dialect where '[' is not an identifier
// quote at all, so it reaches here verbatim; typeBase strips it first, and
// unquoteTypeIdent only ever removes a matched pair of the canonical '"' that
// WRAPS what remains. Neither step can see the other's marker. "text"[] — both
// at once — is locked in delimited_type_names_test.go.
//
// HONESTY IS PRESERVED. This maps a delimited SPELLING onto the same lookup key
// as the bare word; it never widens the vocabulary. An unrecognized keyword,
// delimited or not, still lands on db.TypeUnknown, and so does a form that is
// not exactly ONE quoted identifier (a schema-qualified user type such as
// [dbo].[MyType], which canonicalizes to "dbo"."MyType").
func typeLookupKey(raw string) string {
	return unquoteTypeIdent(typeBase(raw))
}

// unquoteTypeIdent removes the canonical ANSI '"' delimiters wrapping s, when s
// is EXACTLY ONE quoted identifier — opened and closed by '"' with no further
// '"' inside. Anything else (an undelimited name, a dotted multi-part name, an
// identifier carrying a doubled '"' escape) is returned unchanged, so a name
// this function cannot read whole is never half-stripped into a guess.
func unquoteTypeIdent(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	inner := s[1 : len(s)-1]
	if strings.ContainsRune(inner, '"') {
		return s
	}
	return inner
}
