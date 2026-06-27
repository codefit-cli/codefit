package findings_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// The fingerprint is a content identity: category + file + normalized content,
// NO line. It must be stable across line moves and re-indentation, and change
// when the content itself changes.
func TestFingerprintStableAcrossWhitespaceAndLine(t *testing.T) {
	a := findings.Fingerprint("overfetch", "app/users/route.ts", "return Response.json(await prisma.user.findMany());")
	// Same content, different indentation / surrounding whitespace → same fp.
	b := findings.Fingerprint("overfetch", "app/users/route.ts", "  return Response.json(await   prisma.user.findMany());  ")
	if a != b {
		t.Errorf("fingerprint must be stable across whitespace: %q vs %q", a, b)
	}
	if a == "" {
		t.Fatal("fingerprint must not be empty")
	}
}

func TestFingerprintChangesWithContentCategoryFile(t *testing.T) {
	base := findings.Fingerprint("overfetch", "app/users/route.ts", "prisma.user.findMany()")
	cases := map[string]string{
		"different content":  findings.Fingerprint("overfetch", "app/users/route.ts", "prisma.user.findFirst()"),
		"different category": findings.Fingerprint("authz", "app/users/route.ts", "prisma.user.findMany()"),
		"different file":     findings.Fingerprint("overfetch", "app/admin/route.ts", "prisma.user.findMany()"),
	}
	for name, fp := range cases {
		if fp == base {
			t.Errorf("%s must change the fingerprint, got the same %q", name, fp)
		}
	}
}

// SAFETY (C1): two distinct rules on the same source line must get distinct
// fingerprints, or accepting one would silence the other. The producer folds the
// rule ID into the category, so identical content under different ids differs.
func TestFingerprintDistinctRulesSameLineDiffer(t *testing.T) {
	line := "eval(userInput); db.query(userInput)"
	evalFP := findings.Fingerprint("security/SEC-014", "h.ts", line)
	sqlFP := findings.Fingerprint("security/SEC-010", "h.ts", line)
	if evalFP == sqlFP {
		t.Errorf("distinct rules on the same line must not share a fingerprint")
	}
}

// The fingerprint is a one-way hash: a hardcoded secret must never appear in it
// (the baseline file is committed).
func TestFingerprintDoesNotLeakContent(t *testing.T) {
	secret := "sk-ABCDEF0123456789ABCDEF"
	fp := findings.Fingerprint("security", "config.ts", "const k = \""+secret+"\"")
	if len(fp) != 12 {
		t.Errorf("fingerprint should be a short hash (12 hex), got %q", fp)
	}
	if strings.Contains(fp, secret) {
		t.Errorf("fingerprint must not contain the raw content")
	}
}
