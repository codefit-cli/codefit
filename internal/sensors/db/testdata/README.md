# `internal/sensors/db/testdata`

Two corpora, each locking a different way the unread-schema floor can lie:
**the encoding twins** (a file codefit cannot read) and
**`migrations_traceless/`** (files codefit reads perfectly that leave no trace).

## The encoding twins

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

## `migrations_traceless/` — the three states of "left no trace"

A Flyway-shaped migration set, consumed as a **directory** (`flywayOrderedSQL`),
which is the configuration the defect it locks was measured on. It exists because
**no corpus in this repository hosted a migration set at all** — only the three
encoding twins — so nothing could exhibit the shape where codefit reads a file
correctly and it still leaves no position in the model.

| file | state | what it locks |
|---|---|---|
| `V1__initial_schema.sql` | contributes | the only file that puts anything in the model; without it the other four have nothing to be no-ops *against* |
| `V2__user_license_fields.sql` | **(b)** resolved no-op | `ADD COLUMN IF NOT EXISTS` on columns V1 declares — read, reduced correctly, **must not** be reported as blindness |
| `V3__seed_roles.sql` | **(c)** declares no schema | pure `INSERT`/`UPDATE`/`GRANT` — read, no structure in it, **must not** be reported as blindness |
| `V4__widen_email.sql` | **(a)** blindness | `ALTER COLUMN … TYPE` is a *recognized* skip the model does not carry — codefit really did not see it, so it **must** stay under the blindness reason |
| `V5__unknown_form.sql` | **(a)** blindness | `CREATE DOMAIN` reaches the reducer's residual `default:` branch — the same branch DML used to reach. This is the **control**: if "declares no schema" were ever inferred from `default:` instead of from a positive head match, this file would be reported as benign |

The corpus is asserted against by **content**, not by name: `../unread_classification_test.go`
fails loudly if the file count changes, and every state above is asserted
separately. Editing any file here without re-reading that test will silently
weaken it. See ADR 0068.
