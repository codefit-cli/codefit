package paradigm_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// StripRoleToken is the ONE public seam that lets another core package reuse
// this package's role-name vocabulary instead of keeping a second copy of it.
// It exists because a second copy already drifted: internal/core/dwrules kept
// its own three-spelling time-dimension list, and widening the vocabulary here
// turned DW-005 into a confident false claim on two real corpora.
//
// The contract these tests lock: StripRoleToken recognizes EXACTLY the
// spellings candidateRole recognizes — no more, no less — and returns the rest
// of the name with the token removed.

func TestStripRoleToken_LeadingUnderscoreToken(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"dim_date", "date"},
		{"d_date", "date"},
		{"D_DATE", "DATE"},
		{"D_Date", "Date"},
		{"fact_sales", "sales"},
		{"fct_orders", "orders"},
		{"f_jobs", "jobs"},
		{"stg_raw_events", "raw_events"},
		{"mart_finance", "finance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := paradigm.StripRoleToken(tc.name)
			if !ok {
				t.Fatalf("StripRoleToken(%q) ok = false, want true", tc.name)
			}
			if got != tc.want {
				t.Errorf("StripRoleToken(%q) = %q, want %q (the remainder keeps its original spelling)", tc.name, got, tc.want)
			}
		})
	}
}

func TestStripRoleToken_TrailingUnderscoreToken(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"date_dim", "date"},
		{"item_dims", "item"},
		{"sales_fact", "sales"},
		{"store_sales_facts", "store_sales"},
		{"DATE_DIM", "DATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := paradigm.StripRoleToken(tc.name)
			if !ok {
				t.Fatalf("StripRoleToken(%q) ok = false, want true", tc.name)
			}
			if got != tc.want {
				t.Errorf("StripRoleToken(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestStripRoleToken_PascalCase(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"DimDate", "Date"},
		{"DimCustomer", "Customer"},
		{"FactInternetSales", "InternetSales"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := paradigm.StripRoleToken(tc.name)
			if !ok {
				t.Fatalf("StripRoleToken(%q) ok = false, want true", tc.name)
			}
			if got != tc.want {
				t.Errorf("StripRoleToken(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// The word-boundary guard, identical to the one candidateRole enforces: a bare
// English word that merely STARTS with "fact"/"dim" carries no token, and an
// all-caps name is not PascalCase. If StripRoleToken were laxer than
// candidateRole, a caller composing on it would recognize names this package
// deliberately refuses.
func TestStripRoleToken_NoRecognizedToken(t *testing.T) {
	for _, name := range []string{
		"factory_settings", "dimension_config", "dimensional_data", "FactoryOrder",
		"FACTORY_SETTINGS", "DIMENSION_CONFIG",
		"users", "update", "candidate", "validate", "datetime_log",
		"dim", "fact", // a lone token with no remainder is not a table name
	} {
		t.Run(name, func(t *testing.T) {
			if got, ok := paradigm.StripRoleToken(name); ok {
				t.Errorf("StripRoleToken(%q) = (%q, true), want ok = false — %q carries no recognized "+
					"warehouse role token, and a laxer seam here re-opens the false promotions "+
					"TestVocabulary_BareWordPrefixIsNotAToken guards", name, got, name)
			}
		})
	}
}

// StripRoleToken and Detect must never disagree about WHICH names are
// recognized: the whole point of the seam is that there is one vocabulary, not
// two. This walks the same names the vocabulary tests use and asserts the seam
// agrees with the classifier's own name recognition, observed through the
// public Unprovable signal (populated only for a name-recognized table whose
// structure could not be proven).
func TestStripRoleToken_AgreesWithDetectOnRecognition(t *testing.T) {
	recognized := []string{"dim_date", "d_date", "DimDate", "date_dim", "Fact_Sales", "fct_orders"}
	unrecognized := []string{"factory_settings", "dimension_config", "FactoryOrder", "users"}

	for _, name := range recognized {
		if _, ok := paradigm.StripRoleToken(name); !ok {
			t.Errorf("StripRoleToken(%q) ok = false, but the vocabulary tests prove Detect recognizes it", name)
		}
	}
	for _, name := range unrecognized {
		if _, ok := paradigm.StripRoleToken(name); ok {
			t.Errorf("StripRoleToken(%q) ok = true, but Detect deliberately refuses it", name)
		}
	}
}
