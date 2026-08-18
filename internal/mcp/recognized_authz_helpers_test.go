package mcp_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// recognizedHelpersFixtureFile/Src is the smallest TypeScript endpoint that
// resolves a security provider — the fixture shape this file's tests share,
// registering a real authz helper (via a real
// codefit-baseline-register-authz-helper call, never a hand-built struct)
// when a test case needs one recognized.
const recognizedHelpersFixtureFile = "app/x/route.ts"
const recognizedHelpersFixtureSrc = "export async function GET() { return Response.json({}); }\n"

// zeroHelpersNoteSubstring is the spec's VERBATIM zero-case sentence (spec
// scenario "A project with zero registered helpers"): the subject is
// codefit's own knowledge, never the project's authorization state.
const zeroHelpersNoteSubstring = "codefit recognized no project-registered authorization helper for this language"

// TestRecognizedAuthzHelpers_ZeroAndOne is the spec's core response-field
// requirement, driven over REAL HandleScanAll/HandleScanSecurity and a REAL
// baseline (t.TempDir + mustWrite + HandleBaselineRegisterAuthzHelper) —
// never a hand-built response struct, so the test can only pass if the real
// baseline load, the real handler wiring, and the real note-builder all agree.
func TestRecognizedAuthzHelpers_ZeroAndOne(t *testing.T) {
	cases := []struct {
		name        string
		register    bool
		wantHelpers []string
	}{
		{name: "zero registered helpers", register: false, wantHelpers: []string{}},
		{name: "one registered helper", register: true, wantHelpers: []string{"requirePermission"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, root, recognizedHelpersFixtureFile, recognizedHelpersFixtureSrc)
			if tc.register {
				if _, err := mcp.HandleBaselineRegisterAuthzHelper(mcp.BaselineRegisterAuthzHelperRequest{
					Root: root, Language: "typescript", HelperName: "requirePermission", Reason: "project RBAC wrapper",
				}); err != nil {
					t.Fatalf("registering helper: %v", err)
				}
			}

			t.Run("scan-all", func(t *testing.T) {
				resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
				if err != nil {
					t.Fatalf("HandleScanAll: %v", err)
				}
				if !resp.Security.Measured {
					t.Fatalf("security must be measured for a typescript project, got %+v", resp.Security)
				}
				if resp.Security.RecognizedAuthzHelpers == nil {
					t.Fatal("security.recognized_authz_helpers must be present (non-nil pointer) when security is measured")
				}
				assertHelperNames(t, *resp.Security.RecognizedAuthzHelpers, tc.wantHelpers)
				assertHelperNote(t, resp.Security.RecognizedAuthzHelpersNote, len(tc.wantHelpers))

				raw, err := json.Marshal(resp)
				if err != nil {
					t.Fatal(err)
				}
				if len(tc.wantHelpers) == 0 && !strings.Contains(string(raw), `"recognized_authz_helpers":[]`) {
					t.Errorf("the zero-registration case must marshal recognized_authz_helpers as [] (present, "+
						"non-null — codefit LOOKED and found none), got:\n%s", raw)
				}
				if len(tc.wantHelpers) > 0 && !strings.Contains(string(raw), `"recognized_authz_helpers":["requirePermission"]`) {
					t.Errorf("the one-registered case must marshal the exact registered name, got:\n%s", raw)
				}
			})

			t.Run("scan-security", func(t *testing.T) {
				resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
				if err != nil {
					t.Fatalf("HandleScanSecurity: %v", err)
				}
				assertHelperNames(t, resp.RecognizedAuthzHelpers, tc.wantHelpers)
				assertHelperNote(t, resp.RecognizedAuthzHelpersNote, len(tc.wantHelpers))

				raw, err := json.Marshal(resp)
				if err != nil {
					t.Fatal(err)
				}
				if len(tc.wantHelpers) == 0 && !strings.Contains(string(raw), `"recognized_authz_helpers":[]`) {
					t.Errorf("the zero-registration case must marshal recognized_authz_helpers as [] (present, "+
						"non-null), got:\n%s", raw)
				}
			})
		})
	}
}

// TestRecognizedAuthzHelpers_AbsentWhenSecurityNotMeasured is the spec's
// "Security did not run" scenario: an unresolved-language project (python —
// registry.go has no provider for it, same fixture shape as
// scanall_dbonly_test.go's writeUnresolvedSchemaProj) with a configured
// schema still measures DB, but recognized_authz_helpers and its note must be
// ENTIRELY ABSENT from the marshaled JSON — a strings.Contains false check,
// not merely an empty array, per NON-NEGOTIABLE 3's reachability requirement.
func TestRecognizedAuthzHelpers_AbsentWhenSecurityNotMeasured(t *testing.T) {
	root := writeUnresolvedSchemaProj(t)
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.Security.Measured {
		t.Fatal("fixture must produce security.measured=false (no resolvable provider for python) — " +
			"this test would prove nothing about the absent-key case otherwise")
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	// "recognized_authz_helpers" is a substring of "recognized_authz_helpers_note"
	// too, so this single check covers BOTH keys per the spec's "entirely
	// absent from the JSON" requirement.
	if strings.Contains(string(raw), "recognized_authz_helpers") {
		t.Errorf("recognized_authz_helpers and recognized_authz_helpers_note must be entirely absent when "+
			"security is not measured (nobody looked, not looked-and-found-none), got:\n%s", raw)
	}
}

// TestRecognizedAuthzHelpersNote_WordingLock is the spec's "Wording states a
// fact about codefit, never a judgment about the project" requirement,
// against a deny-list of judgment phrases, plus NON-NEGOTIABLE 2: the note
// must make no claim about resolved_clean/actionable's bucket contents — the
// causal link is verified FALSE (idor.go's known_authz_detected gate is an
// OR against the built-in helper set, so a zero registered-helper count does
// not force resolved_clean to 0).
func TestRecognizedAuthzHelpersNote_WordingLock(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, recognizedHelpersFixtureFile, recognizedHelpersFixtureSrc)
	resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	note := strings.ToLower(resp.RecognizedAuthzHelpersNote)

	for _, judgment := range []string{"no authorization", "unprotected", "vulnerable", "unauthenticated"} {
		if strings.Contains(note, judgment) {
			t.Errorf("the zero-case note must state a fact about codefit's knowledge, never a judgment "+
				"about the project (issue #148 precedent): contains forbidden phrase %q, got %q",
				judgment, resp.RecognizedAuthzHelpersNote)
		}
	}
	for _, causal := range []string{"resolved_clean", "actionable"} {
		if strings.Contains(note, causal) {
			t.Errorf("the note must make NO claim about bucket contents (verified false — a built-in "+
				"helper match sets known_authz_detected=true independent of the registered count): "+
				"contains %q, got %q", causal, resp.RecognizedAuthzHelpersNote)
		}
	}
}

func assertHelperNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("recognized_authz_helpers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recognized_authz_helpers = %v, want %v", got, want)
		}
	}
}

func assertHelperNote(t *testing.T, note string, count int) {
	t.Helper()
	if note == "" {
		t.Fatal("recognized_authz_helpers_note must be non-empty whenever the array is present")
	}
	if count == 0 {
		if !strings.Contains(note, zeroHelpersNoteSubstring) {
			t.Errorf("zero-case note must contain %q verbatim, got %q", zeroHelpersNoteSubstring, note)
		}
		if !strings.Contains(note, "codefit-baseline-register-authz-helper") {
			t.Errorf("zero-case note must name codefit-baseline-register-authz-helper as the fix, got %q", note)
		}
		return
	}
	if !strings.Contains(note, fmt.Sprintf("%d", count)) {
		t.Errorf("note for %d recognized helper(s) must state the count, got %q", count, note)
	}
}
