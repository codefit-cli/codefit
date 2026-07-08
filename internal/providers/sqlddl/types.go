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
