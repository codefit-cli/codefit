# `internal/sensors/db/testdata` — the encoding twins

Three files, one schema. They exist because **no other corpus in this repository
could catch an encoding regression**: all 28 that predate them are UTF-8 with no
byte-order mark, measured in Go over every `.sql`/`.prisma` in the tree (three
`rg` probes written for this were found broken first — an invalid-UTF-8 pattern
exiting 2, an empty `$'\x00'` pattern matching every file, and a BOM pattern that
did not fire on a file that has one).

| file | bytes | what it locks |
|---|---|---|
| `twin_utf8.sql` | UTF-8, no mark | the reference parse (2 tables, 2 primary keys, 1 foreign key) |
| `twin_utf16le_bom.sql` | UTF-16LE, `FF FE` | must parse **identically** to the reference — this is what `pg_dump` writes under PowerShell |
| `twin_utf16le_nobom.sql` | UTF-16LE, no mark | must be **declared unread**, never guessed at and never silently accepted |

Two of them are binary as far as `git diff` is concerned. That is expected: the
point of the fixture is the bytes, and a diff that showed them as text would be
lying about what is on disk.

**They are kept in step by their tests, not by hand.** If you edit
`twin_utf8.sql`, regenerate the other two from it — anything else makes the twin
comparison in `../encoding_test.go` meaningless:

```sh
cd internal/sensors/db/testdata
iconv -f UTF-8 -t UTF-16LE twin_utf8.sql > twin_utf16le_nobom.sql
{ printf '\xff\xfe'; cat twin_utf16le_nobom.sql; } > twin_utf16le_bom.sql
```

The three live **under the sensor**, not under `internal/providers/sqlddl`,
because that is where the decode is: the parser is filesystem-free and receives
text (ADR 0014), so a byte-order mark never reaches it. See ADR 0044.
