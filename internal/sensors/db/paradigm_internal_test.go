package db

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// TestToParadigmEnum locks the sensor-local config-string → paradigm.Paradigm
// mapping (design §2c): identity, since config validation already restricts
// the value to auto/oltp/olap/mixed/"" and paradigm.Resolve treats ""/"auto"
// as no-override — so toParadigmEnum need not special-case "" itself.
func TestToParadigmEnum(t *testing.T) {
	cases := map[string]paradigm.Paradigm{
		"":      paradigm.Paradigm(""), // empty stays empty; Resolve treats it as no-override
		"auto":  paradigm.ParadigmAuto,
		"oltp":  paradigm.ParadigmOLTP,
		"olap":  paradigm.ParadigmOLAP,
		"mixed": paradigm.ParadigmMixed,
	}
	for in, want := range cases {
		if got := toParadigmEnum(in); got != want {
			t.Errorf("toParadigmEnum(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestToParadigmEnum_EmptyAndAutoBothResolveAsNoOverride locks the end-to-end
// contract: whatever toParadigmEnum("") returns, paradigm.Resolve treats it
// the same as ParadigmAuto (no override — detection decides).
func TestToParadigmEnum_EmptyAndAutoBothResolveAsNoOverride(t *testing.T) {
	detected := paradigm.Classification{Paradigm: paradigm.ParadigmOLAP}
	gotEmpty := paradigm.Resolve(detected, toParadigmEnum(""))
	gotAuto := paradigm.Resolve(detected, toParadigmEnum("auto"))
	if gotEmpty.Paradigm != paradigm.ParadigmOLAP {
		t.Errorf("Resolve with toParadigmEnum(\"\") = %q, want olap (no override)", gotEmpty.Paradigm)
	}
	if gotAuto.Paradigm != paradigm.ParadigmOLAP {
		t.Errorf("Resolve with toParadigmEnum(\"auto\") = %q, want olap (no override)", gotAuto.Paradigm)
	}
}
