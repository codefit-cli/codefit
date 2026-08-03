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
// Status: INERT. Built and tested; wired to no consumer yet.
package cache
