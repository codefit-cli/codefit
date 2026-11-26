package mcp

import (
	"fmt"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
)

// ParseStatusBlock is a response's account of WHICH analysed files the parser did
// not read whole (ADR 0090, exposing what ADR 0089 detects). An error-recovering
// parser never fails: it returns a tree with the damage marked in it, and every
// rule and surface query then walks that tree. A file it stopped reading, or read
// around an error, produces fewer findings — and fewer findings is
// indistinguishable from a cleaner file unless the response says which files
// those are.
//
// It reuses the JSON names findings.SensorResult already declares, and sits next
// to the response's other fields the way recognized_authz_helpers and its _note
// do:
//
//   - ParseStatusReported is true only when the provider reported a parse status
//     for every analysed file and at least one file was analysed. When false,
//     ParseStatusNote says why, and an empty ParseIncomplete means "not checked"
//     — never "every file was read whole".
//   - ParseIncomplete is ALWAYS a non-nil list (marshals `[]`, never `null`): the
//     files not parsed whole, sorted by path, each with its reason and first
//     affected line.
//   - ParseStatusNote is empty only when there is nothing to say: the status was
//     reported and no file is listed.
//
// Every listed file still counts as audited and in the score: the status adds a
// caveat, it never takes a file out of the audit (S13).
type ParseStatusBlock struct {
	ParseStatusReported bool                      `json:"parse_status_reported"`
	ParseIncomplete     []findings.IncompleteFile `json:"parse_incomplete"`
	ParseStatusNote     string                    `json:"parse_status_note,omitempty"`
	// ParserSettingsOverridden names, sorted, the provider's parser settings
	// set in this process — names only, never values (decision 2 of ADR 0090).
	// Absent when none is set or the provider declares none.
	ParserSettingsOverridden []string `json:"parser_settings_overridden,omitempty"`
}

// The reasons a provider can give, and what they mean, stated once for every note
// that lists files. The names are the provider's (ADR 0089, "The reasons, and
// their order").
const parseReasonsLegend = "Reasons: `stopped:<reason>` (the parser stopped before the end of the file, " +
	"e.g. a memory budget or a node limit), `error_root` (the whole file parsed as an error), `error_node` " +
	"(an error node, reported at the first one), `missing_node` (the parser stood in a missing token), " +
	"`error_flag` (the tree is flagged as holding an error and no error node was found)."

// parseNotReportedByProviderNote is the cause "the provider does not report a
// parse status" (Go today): the files were analysed, nobody checked whether each
// was read whole.
const parseNotReportedByProviderNote = "This language's provider does not report whether each file was " +
	"parsed whole, so `parse_incomplete` is empty because nothing was checked — not because every file " +
	"was read whole."

// parseNoFileAnalysedNote is the cause "no file was analysed in this pass": there
// is no tree, so there is no status.
const parseNoFileAnalysedNote = "No file was analysed in this pass, so there is no parse status: an " +
	"empty `parse_incomplete` here means nothing was parsed, not that every file was read whole."

// parseRequestedFileNotAnalysedNote is scan-endpoint's cause "the requested file
// was not among the files analysed": the run has a status, but none for it.
const parseRequestedFileNotAnalysedNote = "The requested file was not among the files this pass " +
	"analysed, so there is no parse status for it: an empty `parse_incomplete` here is not evidence " +
	"that it was read whole."

// surfaceParseStatusNote is the constant declaration every codefit-surface-*
// tool carries (same pattern as helperScopeNote): those tools parse the files
// they are handed without asking whether each parse was whole.
const surfaceParseStatusNote = "The codefit-surface-* tools do not check whether each file was parsed " +
	"whole: `parse_incomplete` is empty because nothing was checked, not because every file was read " +
	"whole. For the files the parser did not read whole, with the reason and the line, use " +
	"codefit-scan-security or codefit-scan-all."

// parseCrossNote is the sentence scan-all adds when files are listed and the db
// dimension ran: the code x schema cross reads the code with its own parse,
// which reports no status (ADR 0089, "Not covered").
const parseCrossNote = " The code×schema cross in `db` (DB-010/DB-013) reads these files with a " +
	"separate parse that reports no parse status: a cross finding missing from one of them is not " +
	"evidence either."

// parseSettingsNote is the sentence a note carries when parser settings are
// overridden: a non-default setting can hide damage from the status itself (ADR
// 0089, "Not covered"), so a listed-complete file under one proves less.
const parseSettingsNote = "Parser settings are overridden in this process (named in " +
	"`parser_settings_overridden`; values are never reported): a non-default setting can change what " +
	"the parser reads and can hide damage from this status, so a file under one can read as parsed " +
	"whole when it was not."

// withOverriddenSettings declares the language's overridden parser settings on
// the block: their names, and one sentence in the note. The provider is asked
// through providers.SettingsOverrideReporter; a provider without it, or with
// nothing set, leaves the block exactly as it was.
func withOverriddenSettings(block ParseStatusBlock, language string) ParseStatusBlock {
	so, ok := providerForLanguage(language, nil).(providers.SettingsOverrideReporter)
	if !ok {
		return block
	}
	names := so.SettingsOverridden()
	if len(names) == 0 {
		return block
	}
	block.ParserSettingsOverridden = names
	if block.ParseStatusNote != "" {
		block.ParseStatusNote += " "
	}
	block.ParseStatusNote += parseSettingsNote
	return block
}

// parseStatusFor projects a security run's parse status into the response
// block. It is the ONE place the block is built for a security run, so
// scan-security, scan-all and scan-endpoint cannot drift in shape or wording.
func parseStatusFor(res findings.SensorResult) ParseStatusBlock {
	listed := nonNilIncomplete(res.ParseIncomplete)
	block := ParseStatusBlock{ParseStatusReported: res.ParseStatusReported, ParseIncomplete: listed}
	switch {
	case len(listed) > 0:
		block.ParseStatusNote = parseIncompleteNote(len(listed))
		if !res.ParseStatusReported {
			block.ParseStatusNote += " " + parseNotReportedByProviderNote
		}
	case res.ParseStatusReported:
		// Reported and nothing listed: nothing to say.
	case len(res.AuditedFiles) == 0:
		block.ParseStatusNote = parseNoFileAnalysedNote
	default:
		block.ParseStatusNote = parseNotReportedByProviderNote
	}
	return block
}

// parseStatusForFile narrows a run's block to ONE requested file
// (codefit-scan-endpoint): its entry or none. A file the run did not analyse has
// no status, whatever the run reported for the others.
func parseStatusForFile(res findings.SensorResult, file string) ParseStatusBlock {
	want := filepath.Clean(file)
	analysed := false
	for _, f := range res.AuditedFiles {
		if filepath.Clean(f) == want {
			analysed = true
			break
		}
	}
	narrowed := res
	narrowed.ParseIncomplete = nil
	for _, inc := range res.ParseIncomplete {
		if filepath.Clean(inc.File) == want {
			narrowed.ParseIncomplete = append(narrowed.ParseIncomplete, inc)
		}
	}
	if !analysed && res.ParseStatusReported {
		return ParseStatusBlock{
			ParseIncomplete: nonNilIncomplete(narrowed.ParseIncomplete),
			ParseStatusNote: parseRequestedFileNotAnalysedNote,
		}
	}
	return parseStatusFor(narrowed)
}

// surfaceParseStatus is the block every codefit-surface-* response carries.
func surfaceParseStatus() ParseStatusBlock {
	return ParseStatusBlock{ParseIncomplete: []findings.IncompleteFile{}, ParseStatusNote: surfaceParseStatusNote}
}

// parseIncompleteNote states what a non-empty list means.
func parseIncompleteNote(n int) string {
	return fmt.Sprintf("%d analysed file(s) were not parsed whole and are listed in `parse_incomplete` "+
		"with the reason and the first affected line. Each still counts as audited and in the score; its "+
		"findings and surface cover only what the parser read. %s", n, parseReasonsLegend)
}

// parseScopeSentence is the caveat a scope note carries when files are listed
// (S13: a non-empty `scope.note` whenever the list is non-empty).
func parseScopeSentence(n int) string {
	return fmt.Sprintf("%d audited file(s) were not parsed whole and are listed in `parse_incomplete`: each "+
		"still counts as audited and in the score, but its findings and surface cover only what the "+
		"parser read — from the listed line on, a missing finding is not evidence of a clean file.", n)
}

// nonNilIncomplete converts a nil list to an explicit empty one, so the field
// marshals `[]` and never `null` (the same trap nonNilStrings guards).
func nonNilIncomplete(l []findings.IncompleteFile) []findings.IncompleteFile {
	if l == nil {
		return []findings.IncompleteFile{}
	}
	return l
}
