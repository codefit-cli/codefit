package sourcetext_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/sourcetext"
)

// utf16le encodes s as UTF-16LE, optionally prefixed with the FF FE BOM. It is
// deliberately written here (not taken from the production path) so the test
// input is independent of the code under test.
func utf16le(s string, bom bool) []byte {
	var out []byte
	if bom {
		out = append(out, 0xFF, 0xFE)
	}
	for _, r := range s {
		out = append(out, byte(r), 0x00)
	}
	return out
}

func utf16be(s string, bom bool) []byte {
	var out []byte
	if bom {
		out = append(out, 0xFE, 0xFF)
	}
	for _, r := range s {
		out = append(out, 0x00, byte(r))
	}
	return out
}

const ddl = "CREATE TABLE t (id int PRIMARY KEY);\n"

func TestDecode_BOMMarkedEncodings(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		enc  sourcetext.Encoding
		want string
	}{
		{"plain utf-8", []byte(ddl), sourcetext.EncodingUTF8, ddl},
		{"utf-8 with BOM", append([]byte{0xEF, 0xBB, 0xBF}, []byte(ddl)...), sourcetext.EncodingUTF8BOM, ddl},
		{"utf-16le with BOM", utf16le(ddl, true), sourcetext.EncodingUTF16LE, ddl},
		{"utf-16be with BOM", utf16be(ddl, true), sourcetext.EncodingUTF16BE, ddl},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, enc := sourcetext.Decode(c.in)
			if enc != c.enc {
				t.Errorf("encoding = %q, want %q", enc, c.enc)
			}
			if string(got) != c.want {
				t.Errorf("text = %q, want %q", got, c.want)
			}
		})
	}
}

// A file with no BOM is returned BYTE-IDENTICAL. This is the no-regression
// guarantee the whole change rests on: every correctly-encoded corpus in this
// repository takes this path, so Decode must be the identity on it.
func TestDecode_NoBOMIsByteIdentical(t *testing.T) {
	inputs := [][]byte{
		[]byte(ddl),
		{},
		nil,
		{0xFF},                   // a lone FF is not a BOM
		{0xEF, 0xBB},             // a truncated UTF-8 BOM is not a BOM
		[]byte("caf\xe9 latin1"), // invalid UTF-8: never rewritten, never rejected
		utf16le(ddl, false),      // BOM-less UTF-16: DECLARED undecodable, never guessed
	}
	for _, in := range inputs {
		got, enc := sourcetext.Decode(in)
		if enc != sourcetext.EncodingUTF8 {
			t.Errorf("Decode(%q) encoding = %q, want %q", in, enc, sourcetext.EncodingUTF8)
		}
		if !bytes.Equal(got, in) {
			t.Errorf("Decode(%q) = %q, want the input unchanged", in, got)
		}
	}
}

// Decode NEVER guesses a BOM-less UTF-16 file into text. It reports the fact
// through ContainsNUL instead, so the caller DECLARES "not text I can read"
// rather than risking a corrupted read of a Latin-1 or binary-ish file.
func TestContainsNUL_SeparatesDeclaredFromGuessed(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"decoded utf-16le with BOM", mustDecode(t, utf16le(ddl, true)), false},
		{"decoded utf-16be with BOM", mustDecode(t, utf16be(ddl, true)), false},
		{"BOM-less utf-16le", mustDecode(t, utf16le(ddl, false)), true},
		{"BOM-less utf-16be", mustDecode(t, utf16be(ddl, false)), true},
		{"plain ascii", []byte(ddl), false},
		{"latin-1 high bytes", []byte("caf\xe9"), false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sourcetext.ContainsNUL(c.in); got != c.want {
				t.Errorf("ContainsNUL = %v, want %v", got, c.want)
			}
		})
	}
}

// A UTF-16 file whose last code unit is cut in half is still decoded as far as
// it goes: a truncated file is exactly one of the inputs this change exists to
// stop reading as an empty-but-clean schema, so Decode must not return "".
func TestDecode_OddLengthUTF16IsDecodedAsFarAsItGoes(t *testing.T) {
	in := utf16le(ddl, true)
	in = in[:len(in)-1] // cut the final code unit in half
	got, enc := sourcetext.Decode(in)
	if enc != sourcetext.EncodingUTF16LE {
		t.Fatalf("encoding = %q, want %q", enc, sourcetext.EncodingUTF16LE)
	}
	if !strings.HasPrefix(string(got), "CREATE TABLE t (id int PRIMARY KEY);") {
		t.Fatalf("text = %q, want the statement decoded up to the truncation", got)
	}
	if sourcetext.ContainsNUL(got) {
		t.Fatalf("text = %q, want no NUL after a real UTF-16 decode", got)
	}
}

// The BOM alone, with nothing after it, decodes to empty text — a genuinely
// empty file, not an unreadable one.
func TestDecode_BOMOnly(t *testing.T) {
	for _, in := range [][]byte{{0xEF, 0xBB, 0xBF}, {0xFF, 0xFE}, {0xFE, 0xFF}} {
		got, _ := sourcetext.Decode(in)
		if len(got) != 0 {
			t.Errorf("Decode(% x) = %q, want empty", in, got)
		}
	}
}

func mustDecode(t *testing.T, in []byte) []byte {
	t.Helper()
	out, _ := sourcetext.Decode(in)
	return out
}
