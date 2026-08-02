// Package sourcetext decodes the RAW BYTES of a file codefit was asked to read
// into the UTF-8 text every layer above it assumes it already has.
//
// It exists because every reader in codefit used to hand os.ReadFile's bytes
// straight to a tokenizer. On a schema dump written by pg_dump under PowerShell
// — UTF-16LE with a byte-order mark, an entirely ordinary thing to find on a
// Windows machine — that produced the worst state an auditor can occupy: nine
// tables, nine primary keys and eleven foreign keys read as ZERO of each, with
// Measured=true, score 100 and no note. Indistinguishable, to the agent reading
// the result, from a clean bill of health.
//
// The package is a LEAF: it imports nothing from codefit and only the standard
// library (unicode/utf16). It is deliberately tiny and deliberately timid — see
// [Decode] for the one thing it refuses to do.
package sourcetext
