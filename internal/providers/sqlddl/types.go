package sqlddl

import "strings"

import "github.com/codefit-cli/codefit/internal/core/db"

// mapSQLType maps a PostgreSQL base type name to the neutral db.Type. The raw type
// is preserved separately on the column. An unknown type is TypeUnknown (honest) —
// the declared subset grows by dogfooded demand, not speculation.
func mapSQLType(base string) db.Type {
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "bigserial", "serial", "smallserial", "bigint", "integer", "int", "int2", "int4", "int8", "smallint":
		return db.TypeInt
	case "boolean", "bool":
		return db.TypeBool
	case "timestamp", "timestamptz", "timestamp with time zone", "timestamp without time zone", "date", "time", "timetz":
		return db.TypeDateTime
	case "numeric", "decimal", "real", "double precision", "double", "float", "float4", "float8", "money":
		return db.TypeFloat
	case "json", "jsonb":
		return db.TypeJSON
	case "bytea":
		return db.TypeBytes
	case "uuid":
		return db.TypeString
	case "text":
		return db.TypeText
	case "varchar", "character varying", "char", "character", "bpchar", "citext", "name":
		return db.TypeString
	default:
		return db.TypeUnknown
	}
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
