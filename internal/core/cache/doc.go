// Package cache is the content-hash finding cache (PRD §19, optimization 2).
// Each analysed file's RAW output — findings and mapped surface, before path
// criticality is applied — is stored under a key naming the analyzer, the file's
// path and the file's bytes. A hit means the same analyzer already analysed
// exactly those bytes at exactly that path, so a recurring full scan costs about
// as much as an incremental one.
//
// That affordability is the point, not speed for its own sake: the full scan is
// the honest one — the only scan that can prune the baseline, and the only one
// whose "not blocked" means what it appears to mean. If the full scan were
// expensive and the narrowed one cheap, every caller would narrow, and codefit
// would degrade into a tool that permanently looks through a slit.
//
// Two invariants govern everything here:
//
//   - A warm scan and a cold scan are IDENTICAL, not merely equivalent. That is
//     why an [Entry] carries the surface as well as the findings, and why the key
//     names the file's path as well as its bytes.
//   - The cache is never the reason an audit does not happen. A missing,
//     unreadable or corrupt entry is a miss; a failed write is a note.
//
// Entries are stored by GENERATION — Dir/<generation>/<key>.json, where the
// generation labels the analyzer that wrote them. That is what makes the store
// collectable: because the analyzer identity is part of every key, each codefit
// build orphans the whole previous generation at once, so the unit that has to
// be droppable is a generation and not an entry. [Open] prunes, once per
// process: the current generation always survives, along with the two most
// recently modified others, and entries in the current generation that have not
// been written in 30 days are collected.
//
// The prune DELETES FILES, so it only ever recognises the two shapes this
// package writes itself — a generation directory of 16 hex characters and an
// entry file of a 64-hex key. Anything else under Dir belongs to whoever put it
// there and is never touched at any age. It is also best effort and reports
// nothing: a cache that cannot clean itself still has to work.
//
// It is wired into the security sensor's walk, consulted per file, and OPT-IN:
// a project with no cache: section in .codefit.yaml has it off. The database
// dimension is deliberately not cached — its inputs are configured schema paths
// rather than a walk, and a schema reconstructed from an ordered set of
// migrations does not obviously invalidate per file.
package cache
