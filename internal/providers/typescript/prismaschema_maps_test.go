package typescript_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
)

func parseMaps(t *testing.T) *db.Schema { t.Helper(); return parseFixture(t, "maps_and_limits.prisma") }

// PARTE 1 — @@map / @map modeled as DBName (empty = no remap).
func TestMap_TableAndColumn(t *testing.T) {
	s := parseMaps(t)
	acct := tableByName(t, s, "Account")
	if acct.DBName != "accounts" {
		t.Errorf("Account.DBName = %q, want accounts (@@map)", acct.DBName)
	}
	if got := columnByName(t, acct, "ref").DBName; got != "reference_code" {
		t.Errorf("Account.ref.DBName = %q, want reference_code (@map)", got)
	}
	// No remap → DBName stays empty (NOT defaulted to Name).
	if got := tableByName(t, s, "User").DBName; got != "" {
		t.Errorf("User.DBName = %q, want empty (no @@map)", got)
	}
	if got := columnByName(t, acct, "id").DBName; got != "" {
		t.Errorf("Account.id.DBName = %q, want empty (no @map)", got)
	}
}

// PARTE 2 — declared limits locked as contract: each construct parses OK and
// alters nothing it shouldn't.

func TestDeclaredLimits_DefaultsIgnored(t *testing.T) {
	s := parseMaps(t)
	acct := tableByName(t, s, "Account")
	if c := columnByName(t, acct, "id"); c.Type != db.TypeInt || c.Nullable {
		t.Errorf("Account.id with @default(autoincrement()) = {%s, null=%v}, want {int, false}", c.Type, c.Nullable)
	}
	if c := columnByName(t, acct, "createdAt"); c.Type != db.TypeDateTime || c.Nullable {
		t.Errorf("Account.createdAt with @default(now()) = {%s, null=%v}, want {datetime, false}", c.Type, c.Nullable)
	}
	if c := columnByName(t, tableByName(t, s, "User"), "id"); c.Type != db.TypeString {
		t.Errorf("User.id with @default(cuid()) Type = %s, want string", c.Type)
	}
}

func TestDeclaredLimits_UpdatedAtIgnored(t *testing.T) {
	c := columnByName(t, tableByName(t, parseMaps(t), "Account"), "updatedAt")
	if c.Type != db.TypeDateTime {
		t.Errorf("Account.updatedAt (@updatedAt) Type = %s, want datetime", c.Type)
	}
}

func TestDeclaredLimits_OnDeleteIgnored(t *testing.T) {
	acct := tableByName(t, parseMaps(t), "Account")
	var owner *db.ForeignKey
	for i := range acct.ForeignKeys {
		if equalStrings(acct.ForeignKeys[i].Columns, []string{"ownerId"}) {
			owner = &acct.ForeignKeys[i]
		}
	}
	if owner == nil {
		t.Fatalf("Account FK on [ownerId] not found; FKs = %+v", acct.ForeignKeys)
	}
	if owner.RefTable != "User" || !equalStrings(owner.RefColumns, []string{"id"}) {
		t.Errorf("owner FK = %+v, want -> User[id] (onDelete must not break it)", *owner)
	}
}

func TestDeclaredLimits_ImplicitM2MNoFK(t *testing.T) {
	s := parseMaps(t)
	acct := tableByName(t, s, "Account")
	// Only the explicit belongs-to (owner) is a FK; the m2m (tags) is not.
	if len(acct.ForeignKeys) != 1 {
		t.Errorf("Account.ForeignKeys = %d, want 1 (only owner; implicit m2m tags must NOT create a FK)", len(acct.ForeignKeys))
	}
	if got := tableByName(t, s, "Tag").ForeignKeys; len(got) != 0 {
		t.Errorf("Tag.ForeignKeys = %+v, want empty (implicit m2m side must NOT create a FK)", got)
	}
	// The virtual relation fields are not columns.
	for _, bad := range []string{"owner", "tags"} {
		for _, c := range acct.Columns {
			if c.Name == bad {
				t.Errorf("Account column %q should not exist (relation field, not a column)", bad)
			}
		}
	}
	if got := columnNames(acct); !equalStrings(got, []string{"id", "ref", "createdAt", "updatedAt", "ownerId"}) {
		t.Errorf("Account columns = %v, want [id ref createdAt updatedAt ownerId]", got)
	}
}
