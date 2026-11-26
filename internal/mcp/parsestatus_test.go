package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/mcp"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

// These tests drive the REAL handlers over the REAL security sensor and the REAL
// TypeScript parser (ADR 0090). Every parse status below was computed by the
// parser over a file on disk; none is a hand-built value.

// lateLine is an AST-only SEC-001 site in TypeScript: the provider's rules fire
// on it and layer 1's regexes do not, so whether it is reported says whether the
// parser read its line. (Its Go name avoids the credential vocabulary on purpose:
// codefit audits its own tree.)
const lateLine = `const apiKey = "s3cr3t";`

// brokenTS has an ERROR node on line 3 (`}}}}`) and the SEC-001 site on line 4,
// the measured shape of the sensor's own fixture: the error is local, so the
// later finding is still reported — and the file is still listed.
const brokenTS = "const a = 1;\nconst b = 2;\n}}}}\n" + lateLine + "\n"

// brokenFiles is a project with one broken and one clean TypeScript file.
func brokenFiles() map[string]string {
	return map[string]string{
		"src/broken.ts": brokenTS,
		"src/clean.ts":  "export const ok = 1;\n",
	}
}

var wantBrokenIncomplete = []findings.IncompleteFile{
	{File: "src/broken.ts", Reason: findings.ParseReasonErrorNode, Line: 3},
}

// rawObject marshals v and decodes it into a generic map, so a test reads the
// bytes an agent receives — `[]` and `null` are different there, and a missing
// key is not a zero value.
func rawObject(t *testing.T, v any) (map[string]json.RawMessage, string) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m, string(b)
}

func hasSEC001At(fs []findings.Finding, file string, line int) bool {
	for _, f := range fs {
		if f.ID == "SEC-001" && f.File == file && f.Line == line {
			return true
		}
	}
	return false
}

// TestScanSecurity_BrokenFileIsListedWithReasonAndLine (S13): a file the parser
// did not read whole is listed with its reason and first line; scope.note says
// what that means; and the file still counts — `audited`, the score and
// `blocked` are computed exactly as they would be without the status.
func TestScanSecurity_BrokenFileIsListedWithReasonAndLine(t *testing.T) {
	root := writeProj(t, brokenFiles())
	resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanSecurity: %v", err)
	}
	if !resp.ParseStatusReported {
		t.Error("parse_status_reported = false for a TypeScript run that analysed two files")
	}
	if !reflect.DeepEqual(resp.ParseIncomplete, wantBrokenIncomplete) {
		t.Errorf("parse_incomplete = %+v, want %+v", resp.ParseIncomplete, wantBrokenIncomplete)
	}
	for _, want := range []string{"1 analysed file(s)", "`stopped:<reason>`", "`error_root`", "`error_node`", "`missing_node`", "`error_flag`"} {
		if !strings.Contains(resp.ParseStatusNote, want) {
			t.Errorf("parse_status_note does not name %q: %q", want, resp.ParseStatusNote)
		}
	}
	if resp.Scope.Mode != mcp.ScopeModeFull {
		t.Fatalf("scope.mode = %q, want full", resp.Scope.Mode)
	}
	for _, want := range []string{"still counts as audited and in the score", "only what the parser read", "a missing finding is not evidence of a clean file"} {
		if !strings.Contains(resp.Scope.Note, want) {
			t.Errorf("scope.note does not say %q: %q", want, resp.Scope.Note)
		}
	}
	// The status is a caveat, never an exclusion.
	if resp.Scope.Audited != 2 || resp.Scope.AuditableTotal != 2 {
		t.Errorf("scope audited=%d auditable_total=%d, want 2 and 2 — an incomplete file still counts",
			resp.Scope.Audited, resp.Scope.AuditableTotal)
	}
	if !hasSEC001At(resp.Findings, "src/broken.ts", 4) {
		t.Fatalf("positive control: the SEC-001 after the local error is not reported: %+v", resp.Findings)
	}
	if want := scoring.DimensionScore(resp.Findings); resp.Score != want {
		t.Errorf("score = %d, want scoring.DimensionScore(findings) = %d", resp.Score, want)
	}
	if want := scoring.IsBlocked(resp.Findings); resp.Blocked != want {
		t.Errorf("blocked = %v, want scoring.IsBlocked(findings) = %v", resp.Blocked, want)
	}
}

// TestScanSecurity_CleanRunShowsEmptyListAndNoNote is the NEGATIVE fixture: a
// TypeScript run whose files all parse whole. It enumerates what the response
// does NOT show:
//
//   - `parse_incomplete` is `[]` on the wire, never `null` and never absent;
//   - no `parse_status_note` key (there is nothing to say);
//   - no `parser_settings_overridden` key (none is set in this process);
//   - `scope.note` is empty, exactly as before parse status existed;
//   - findings and `blocked` are what the same file yields anyway.
func TestScanSecurity_CleanRunShowsEmptyListAndNoNote(t *testing.T) {
	clearParserEnv(t)
	root := writeProj(t, map[string]string{"src/clean.ts": lateLine + "\n"})
	resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanSecurity: %v", err)
	}
	m, raw := rawObject(t, resp)
	if got := string(m["parse_status_reported"]); got != "true" {
		t.Errorf("parse_status_reported = %s, want true: %s", got, raw)
	}
	if got := string(m["parse_incomplete"]); got != "[]" {
		t.Errorf("parse_incomplete = %q on the wire, want [] (never null, never absent): %s", got, raw)
	}
	for _, absent := range []string{"parse_status_note", "parser_settings_overridden"} {
		if _, ok := m[absent]; ok {
			t.Errorf("a clean run carries %q: %s", absent, raw)
		}
	}
	if resp.Scope.Note != "" {
		t.Errorf("scope.note = %q on a clean full run, want empty", resp.Scope.Note)
	}
	if !hasSEC001At(resp.Findings, "src/clean.ts", 1) {
		t.Errorf("positive control: the SEC-001 in the clean file is not reported: %+v", resp.Findings)
	}
	if want := scoring.IsBlocked(resp.Findings); resp.Blocked != want {
		t.Errorf("blocked = %v, want %v", resp.Blocked, want)
	}
}

// TestScanSecurity_GoIsNotReported: the Go provider reports no parse status, and
// the response says so in words — `false`, `[]`, and a note that the list is
// empty because nothing was checked. The scope note stays empty: nothing is
// listed, so the scope has nothing to caveat.
func TestScanSecurity_GoIsNotReported(t *testing.T) {
	root := writeProj(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "go"})
	if err != nil {
		t.Fatalf("HandleScanSecurity: %v", err)
	}
	m, raw := rawObject(t, resp)
	if got := string(m["parse_status_reported"]); got != "false" {
		t.Errorf("parse_status_reported = %s for Go, want false: %s", got, raw)
	}
	if got := string(m["parse_incomplete"]); got != "[]" {
		t.Errorf("parse_incomplete = %q, want []: %s", got, raw)
	}
	if !strings.Contains(resp.ParseStatusNote, "does not report whether each file was parsed whole") ||
		!strings.Contains(resp.ParseStatusNote, "nothing was checked") {
		t.Errorf("parse_status_note does not say the status was not checked: %q", resp.ParseStatusNote)
	}
	if resp.Scope.Note != "" {
		t.Errorf("scope.note = %q, want empty", resp.Scope.Note)
	}
	if resp.Scope.Audited != 1 {
		t.Fatalf("positive control: scope.audited = %d, want 1 (the Go file was analysed)", resp.Scope.Audited)
	}
}

// TestScanSecurity_UnmatchedOnlyScopeIsNotReported: a narrowed run whose scope
// reaches no file analyses nothing, so it has no status — through the handler,
// with the TypeScript provider that would report one for any file it parsed.
func TestScanSecurity_UnmatchedOnlyScopeIsNotReported(t *testing.T) {
	root := writeProj(t, brokenFiles())
	resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{
		Root: root, Language: "typescript", ChangedFiles: []string{"src/does-not-exist.ts"},
	})
	if err != nil {
		t.Fatalf("HandleScanSecurity: %v", err)
	}
	if resp.Scope.Mode != mcp.ScopeModePartial || resp.Scope.Audited != 0 {
		t.Fatalf("positive control: want a partial scope with 0 audited, got %+v", resp.Scope)
	}
	if resp.ParseStatusReported {
		t.Error("parse_status_reported = true for a run that analysed no file")
	}
	if len(resp.ParseIncomplete) != 0 || resp.ParseIncomplete == nil {
		t.Errorf("parse_incomplete = %#v, want an empty non-nil list", resp.ParseIncomplete)
	}
	if !strings.Contains(resp.ParseStatusNote, "No file was analysed") {
		t.Errorf("parse_status_note does not say no file was analysed: %q", resp.ParseStatusNote)
	}
}

// TestScanSecurity_PartialScopeAppendsTheParseSentence: under a partial scope
// whose files include a broken one, the parse caveat is appended to the partial
// note, not substituted for it.
func TestScanSecurity_PartialScopeAppendsTheParseSentence(t *testing.T) {
	root := writeProj(t, brokenFiles())
	resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{
		Root: root, Language: "typescript", ChangedFiles: []string{"src/broken.ts"},
	})
	if err != nil {
		t.Fatalf("HandleScanSecurity: %v", err)
	}
	if !strings.HasPrefix(resp.Scope.Note, "Partial audit: 1 of 2") {
		t.Errorf("scope.note lost the partial note: %q", resp.Scope.Note)
	}
	if !strings.Contains(resp.Scope.Note, "a missing finding is not evidence of a clean file") {
		t.Errorf("scope.note lacks the parse caveat: %q", resp.Scope.Note)
	}
	if !reflect.DeepEqual(resp.ParseIncomplete, wantBrokenIncomplete) {
		t.Errorf("parse_incomplete = %+v, want %+v", resp.ParseIncomplete, wantBrokenIncomplete)
	}
}

// TestScanAll_ParseIncompleteUnderSecurity: scan-all carries the same fields
// inside `security` (present because security was measured), the scope note
// carries the caveat, and — because the db dimension ran here — the note names
// the code×schema cross, which parses the code separately and reports no status.
func TestScanAll_ParseIncompleteUnderSecurity(t *testing.T) {
	files := brokenFiles()
	files[".codefit.yaml"] = regressionTSYAMLWithDB
	files["prisma/schema.prisma"] = regressionNoPKSchema
	root := writeProj(t, files)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("positive control: the db dimension did not run: %+v", resp.DB)
	}
	sec := resp.Security
	if sec.ParseStatusReported == nil || !*sec.ParseStatusReported {
		t.Errorf("security.parse_status_reported = %v, want true", sec.ParseStatusReported)
	}
	if sec.ParseIncomplete == nil || !reflect.DeepEqual(*sec.ParseIncomplete, wantBrokenIncomplete) {
		t.Errorf("security.parse_incomplete = %v, want %+v", sec.ParseIncomplete, wantBrokenIncomplete)
	}
	if !strings.Contains(sec.ParseStatusNote, "1 analysed file(s)") || !strings.Contains(sec.ParseStatusNote, "code×schema cross") {
		t.Errorf("security.parse_status_note does not name the count and the db cross: %q", sec.ParseStatusNote)
	}
	if !strings.Contains(resp.Scope.Note, "a missing finding is not evidence of a clean file") {
		t.Errorf("scope.note lacks the parse caveat: %q", resp.Scope.Note)
	}
	// Under a full scope `audited` is the security walk's count, exactly as
	// before parse status existed: the broken file is in it.
	if resp.Scope.Audited != 2 {
		t.Errorf("scope.audited = %d, want 2 (both code files, the broken one included)", resp.Scope.Audited)
	}
}

// TestScanAll_NotMeasuredSecurityCarriesNoParseKeys: when no security provider
// resolves, `security.measured: false` already says the code was not audited;
// the parse keys are absent rather than present and misleading.
func TestScanAll_NotMeasuredSecurityCarriesNoParseKeys(t *testing.T) {
	root := writeUnresolvedSchemaProj(t)
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.Security.Measured {
		t.Fatal("positive control: security measured for a language with no provider")
	}
	m, raw := rawObject(t, resp.Security)
	for _, absent := range []string{"parse_status_reported", "parse_incomplete", "parse_status_note", "parser_settings_overridden"} {
		if _, ok := m[absent]; ok {
			t.Errorf("an unmeasured security section carries %q: %s", absent, raw)
		}
	}
}

// TestScanEndpoint_ListsOnlyTheRequestedFile: scan-endpoint narrows the run's
// list to the file it was asked about — the broken file gets its entry, the
// clean one gets an empty list and no note, and a file the run never analysed
// gets no status at all.
func TestScanEndpoint_ListsOnlyTheRequestedFile(t *testing.T) {
	root := writeProj(t, brokenFiles())

	broken, err := mcp.HandleScanEndpoint(mcp.ScanEndpointRequest{Root: root, Language: "typescript", File: "src/broken.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if !broken.ParseStatusReported || !reflect.DeepEqual(broken.ParseIncomplete, wantBrokenIncomplete) {
		t.Errorf("broken file: reported=%v parse_incomplete=%+v, want true and %+v",
			broken.ParseStatusReported, broken.ParseIncomplete, wantBrokenIncomplete)
	}
	if !strings.Contains(broken.ParseStatusNote, "1 analysed file(s)") {
		t.Errorf("broken file: parse_status_note = %q", broken.ParseStatusNote)
	}

	clean, err := mcp.HandleScanEndpoint(mcp.ScanEndpointRequest{Root: root, Language: "typescript", File: "src/clean.ts"})
	if err != nil {
		t.Fatal(err)
	}
	m, raw := rawObject(t, clean)
	if string(m["parse_status_reported"]) != "true" || string(m["parse_incomplete"]) != "[]" {
		t.Errorf("clean file: want reported true and [], got %s", raw)
	}
	if _, ok := m["parse_status_note"]; ok {
		t.Errorf("clean file carries a parse_status_note: %s", raw)
	}

	missing, err := mcp.HandleScanEndpoint(mcp.ScanEndpointRequest{Root: root, Language: "typescript", File: "src/nope.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if missing.ParseStatusReported || len(missing.ParseIncomplete) != 0 {
		t.Errorf("unanalysed file: reported=%v list=%+v, want false and empty", missing.ParseStatusReported, missing.ParseIncomplete)
	}
	if !strings.Contains(missing.ParseStatusNote, "not among the files this pass analysed") {
		t.Errorf("unanalysed file: parse_status_note = %q", missing.ParseStatusNote)
	}
}

// TestSurfaceTools_DeclareNoParseStatus: the four stateless surface tools parse
// what they are handed without asking whether each parse was whole. Each says so
// — `false`, `[]`, and the constant note pointing to scan-security / scan-all —
// even over the broken file, which they do not list.
func TestSurfaceTools_DeclareNoParseStatus(t *testing.T) {
	req := mcp.SurfaceIDORRequest{Files: []mcp.FileInput{{Path: "src/broken.ts", Content: brokenTS}}}
	for name, h := range map[string]func(mcp.SurfaceIDORRequest) (mcp.SurfaceResponse, error){
		"idor":      mcp.HandleSurfaceIDOR,
		"authz":     mcp.HandleSurfaceAuthz,
		"overfetch": mcp.HandleSurfaceOverfetch,
		"nplus1":    mcp.HandleSurfaceNPlus1,
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := h(req)
			if err != nil {
				t.Fatal(err)
			}
			m, raw := rawObject(t, resp)
			if string(m["parse_status_reported"]) != "false" || string(m["parse_incomplete"]) != "[]" {
				t.Errorf("want parse_status_reported false and parse_incomplete [], got %s", raw)
			}
			if !strings.Contains(resp.ParseStatusNote, "do not check whether each file was parsed whole") ||
				!strings.Contains(resp.ParseStatusNote, "codefit-scan-security or codefit-scan-all") {
				t.Errorf("parse_status_note = %q", resp.ParseStatusNote)
			}
		})
	}
}

// TestServerProtocolEndToEnd_ParseIncomplete (S16): the fields survive the wire.
// A real MCP client calls the real server over the in-memory transport — the
// same Server.Run path as stdio, with the SDK's output-schema validation in the
// way — and reads the structured result as raw JSON by its literal key names, so
// a renamed tag fails here even though a decode into the same struct would still
// round-trip.
func TestServerProtocolEndToEnd_ParseIncomplete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := writeProj(t, brokenFiles())

	serverT, clientT := mcpsdk.NewInMemoryTransports()
	go func() { _ = mcp.NewServer().Run(ctx, serverT) }()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "codefit-itest", Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect/handshake: %v", err)
	}
	defer func() { _ = session.Close() }()

	call := func(tool string) map[string]json.RawMessage {
		t.Helper()
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      tool,
			Arguments: map[string]any{"root": root, "language": "typescript"},
		})
		if err != nil {
			t.Fatalf("tools/call %s: %v", tool, err)
		}
		if res.IsError {
			t.Fatalf("%s reported an error: %+v", tool, res.Content)
		}
		m, _ := rawObject(t, res.StructuredContent)
		return m
	}
	// The client decodes the structured result into a generic map, so the keys
	// of each entry come back sorted when it is marshalled again here.
	wantList := `[{"file":"src/broken.ts","line":3,"reason":"error_node"}]`
	assertParseKeys := func(where string, m map[string]json.RawMessage) {
		t.Helper()
		if got := string(m["parse_status_reported"]); got != "true" {
			t.Errorf("%s: parse_status_reported = %q on the wire, want true", where, got)
		}
		if got := string(m["parse_incomplete"]); got != wantList {
			t.Errorf("%s: parse_incomplete = %s on the wire, want %s", where, got, wantList)
		}
		var note string
		if err := json.Unmarshal(m["parse_status_note"], &note); err != nil || !strings.Contains(note, "1 analysed file(s)") {
			t.Errorf("%s: parse_status_note = %s (%v)", where, m["parse_status_note"], err)
		}
	}
	scopeNote := func(where string, m map[string]json.RawMessage) {
		t.Helper()
		var scope struct {
			Mode string `json:"mode"`
			Note string `json:"note"`
		}
		if err := json.Unmarshal(m["scope"], &scope); err != nil {
			t.Fatalf("%s: scope: %v", where, err)
		}
		if scope.Mode != "full" || !strings.Contains(scope.Note, "a missing finding is not evidence of a clean file") {
			t.Errorf("%s: scope = %+v, want full with the parse caveat", where, scope)
		}
	}

	sec := call("codefit-scan-security")
	assertParseKeys("codefit-scan-security", sec)
	scopeNote("codefit-scan-security", sec)

	all := call("codefit-scan-all")
	var security map[string]json.RawMessage
	if err := json.Unmarshal(all["security"], &security); err != nil {
		t.Fatalf("codefit-scan-all: security: %v", err)
	}
	assertParseKeys("codefit-scan-all security", security)
	scopeNote("codefit-scan-all", all)
}

// clearParserEnv unsets, for the duration of the test, every parser setting the
// process inherited, so an environment that sets one cannot decide these tests.
// The real provider names them; t.Setenv records each for restoration first.
func clearParserEnv(t *testing.T) {
	t.Helper()
	for _, name := range typescript.New().SettingsOverridden() {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

// TestParseStatus_OverriddenSettingsAreNamedNotValued (decision 2 of ADR 0090):
// a parser setting set in the process is NAMED in `parser_settings_overridden`
// — in scan-security's response and in scan-all's `security` — with one sentence
// in the note, and its value appears nowhere in the response. Without it the key
// is absent. The probe name is one the parser does not read, so setting it
// cannot change how this or any other test parses.
func TestParseStatus_OverriddenSettingsAreNamedNotValued(t *testing.T) {
	clearParserEnv(t)
	const name, value = "GOT_CODEFIT_OVERRIDE_PROBE", "probe-value-7f3a"
	root := writeProj(t, map[string]string{"src/clean.ts": "export const ok = 1;\n"})

	before, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if m, raw := rawObject(t, before); m["parser_settings_overridden"] != nil {
		t.Fatalf("no parser setting is set, yet the key is present: %s", raw)
	}

	t.Setenv(name, value)
	sec, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	all, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	for where, v := range map[string]any{"codefit-scan-security": sec, "codefit-scan-all security": all.Security} {
		m, raw := rawObject(t, v)
		if got := string(m["parser_settings_overridden"]); got != `["`+name+`"]` {
			t.Errorf("%s: parser_settings_overridden = %s, want [%q]", where, got, name)
		}
		if strings.Contains(raw, value) {
			t.Errorf("%s: the setting's VALUE reached the response: %s", where, raw)
		}
		var note string
		_ = json.Unmarshal(m["parse_status_note"], &note)
		if !strings.Contains(note, "`parser_settings_overridden`") {
			t.Errorf("%s: parse_status_note does not mention the overridden settings: %q", where, note)
		}
	}
	if _, raw := rawObject(t, all); strings.Contains(raw, value) {
		t.Errorf("the setting's VALUE reached the scan-all response: %s", raw)
	}
}

// jsonTags returns the JSON names of a struct type's fields, following embedded
// structs the way encoding/json flattens them.
func jsonTags(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			out = append(out, jsonTags(f.Type)...)
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

// TestToolDescriptionsNameRealParseFields (S14): the descriptions an agent reads
// over tools/list name the parse-status fields by the JSON names the responses
// really carry — reflected from ParseStatusBlock, and from SecuritySection for
// scan-all's `security.`-prefixed spelling — so renaming a tag without the
// description, or the reverse, fails here.
func TestToolDescriptionsNameRealParseFields(t *testing.T) {
	block := jsonTags(reflect.TypeOf(mcp.ParseStatusBlock{}))
	if len(block) != 4 {
		t.Fatalf("positive control: ParseStatusBlock has %d JSON fields (%v), want 4", len(block), block)
	}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(mcp.ScanResponse{}), reflect.TypeOf(mcp.SecuritySection{}),
		reflect.TypeOf(mcp.ScanEndpointResponse{}), reflect.TypeOf(mcp.SurfaceResponse{}),
	} {
		got := strings.Join(jsonTags(typ), " ")
		for _, tag := range block {
			if !strings.Contains(" "+got+" ", " "+tag+" ") {
				t.Errorf("%s does not carry the JSON field %q (has: %s)", typ.Name(), tag, got)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	go func() { _ = mcp.NewServer().Run(ctx, serverT) }()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "codefit-itest", Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	lt, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	desc := map[string]string{}
	for _, tool := range lt.Tools {
		desc[tool.Name] = tool.Description
	}

	for _, tool := range []string{"codefit-scan-security", "codefit-scan-endpoint"} {
		for _, tag := range block {
			if !strings.Contains(desc[tool], "`"+tag+"`") {
				t.Errorf("%s's description does not name `%s`", tool, tag)
			}
		}
	}
	for _, tag := range block {
		if !strings.Contains(desc["codefit-scan-all"], "`security."+tag+"`") {
			t.Errorf("codefit-scan-all's description does not name `security.%s`", tag)
		}
	}
	for _, tool := range []string{"codefit-surface-idor", "codefit-surface-authz", "codefit-surface-overfetch", "codefit-surface-nplus1"} {
		if !strings.Contains(desc[tool], "`parse_incomplete`") ||
			!strings.Contains(desc[tool], "do not check whether each file was parsed whole") {
			t.Errorf("%s's description does not declare that it does not check the parse status: %q", tool, desc[tool])
		}
	}
}

// TestSkillTeachesTheRealParseStatusFields (S14): the skill `codefit init`
// generates names the parse-status fields by the JSON names the responses
// really carry — reflected from ParseStatusBlock, never retyped — and the reasons
// by the core's own constants, for every language whose skill teaches the scan
// tools. Renaming a tag, or dropping the skill's paragraph, fails here; the
// committed render is held to the template by TestCommittedSkillMatchesRenderedSkill.
func TestSkillTeachesTheRealParseStatusFields(t *testing.T) {
	tags := jsonTags(reflect.TypeOf(mcp.ParseStatusBlock{}))
	if len(tags) != 4 {
		t.Fatalf("positive control: ParseStatusBlock has %d JSON fields (%v), want 4", len(tags), tags)
	}
	reasons := []string{
		findings.ParseReasonStoppedPrefix + "<reason>", findings.ParseReasonErrorRoot,
		findings.ParseReasonErrorNode, findings.ParseReasonMissingNode, findings.ParseReasonErrorFlag,
	}
	for _, lang := range []string{"typescript", "go"} {
		skill, err := scaffold.RenderSkill(scaffold.ProjectInfo{Name: "x", Language: lang})
		if err != nil {
			t.Fatalf("RenderSkill(%s): %v", lang, err)
		}
		body := string(skill)
		for _, want := range append(append([]string{}, tags...), reasons...) {
			if !strings.Contains(body, "`"+want+"`") {
				t.Errorf("%s: the skill does not name `%s`", lang, want)
			}
		}
		for _, phrase := range []string{"`parse_status_reported: false`", "a missing finding is not evidence of a clean file"} {
			if !strings.Contains(body, phrase) {
				t.Errorf("%s: the skill does not say %q", lang, phrase)
			}
		}
	}
}
