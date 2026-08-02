package sourcetext

import (
	"bytes"
	"unicode/utf16"
)

// Encoding names how a file DECLARED its bytes, through its byte-order mark.
// It is a declaration, never a guess: [EncodingUTF8] means "no byte-order mark
// was present", not "these bytes were shown to be UTF-8".
type Encoding string

const (
	// EncodingUTF8 is the no-byte-order-mark case. Decode returns such input
	// BYTE-IDENTICAL — including invalid UTF-8 — so a Latin-1 file, a file in
	// a codepage codefit has never heard of, and every correctly encoded file
	// in existence all reach the tokenizer exactly as they did before.
	EncodingUTF8 Encoding = "utf-8"
	// EncodingUTF8BOM is EF BB BF. The mark is stripped; a leading BOM is not
	// content, and it is the difference between a first statement the
	// tokenizer reads and one it does not.
	EncodingUTF8BOM Encoding = "utf-8 with a byte-order mark"
	// EncodingUTF16LE is FF FE — what pg_dump writes when its output is
	// redirected by PowerShell, the case that produced this package.
	EncodingUTF16LE Encoding = "utf-16le with a byte-order mark"
	// EncodingUTF16BE is FE FF.
	EncodingUTF16BE Encoding = "utf-16be with a byte-order mark"
)

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// Decode returns data as UTF-8 text plus the encoding its byte-order mark
// declared.
//
// It handles exactly the three BOM-marked encodings and NOTHING ELSE. A file
// with no byte-order mark is returned unchanged, byte for byte, and reported as
// [EncodingUTF8].
//
// WHAT IT DELIBERATELY DOES NOT DO — and this is the load-bearing half of the
// package: it never sniffs a BOM-LESS file. Detecting BOM-less UTF-16 means
// guessing from NUL bytes at regular offsets, and a wrong guess silently
// rewrites the content of a Latin-1, binary-ish, or simply unusual file — a
// corruption strictly worse than the silence it would replace, and one no later
// layer could detect. The honest half of that case is served by [ContainsNUL],
// which lets a caller DECLARE "this is not text I can read" instead of guessing
// what it might have been.
//
// Boundaries, all deliberate:
//
//   - A UTF-16 file whose final code unit is cut in half is decoded as far as
//     it goes and the odd trailing byte is dropped. A truncated file is
//     precisely one of the inputs this package exists to stop reading as an
//     empty-but-clean schema, so returning "" would reintroduce the defect.
//   - Unpaired surrogates become U+FFFD (unicode/utf16's own rule). They are
//     not an error here: the caller's job is to notice how little it recognized,
//     not to validate Unicode.
//   - A UTF-32LE file begins FF FE 00 00, so its BOM is read as UTF-16LE and it
//     decodes to text full of U+0000. That is not a silent misread —
//     [ContainsNUL] reports it, and the caller declares the file unreadable.
func Decode(data []byte) ([]byte, Encoding) {
	switch {
	case bytes.HasPrefix(data, bomUTF8):
		return data[len(bomUTF8):], EncodingUTF8BOM
	case bytes.HasPrefix(data, bomUTF16LE):
		return decodeUTF16(data[len(bomUTF16LE):], true), EncodingUTF16LE
	case bytes.HasPrefix(data, bomUTF16BE):
		return decodeUTF16(data[len(bomUTF16BE):], false), EncodingUTF16BE
	default:
		return data, EncodingUTF8
	}
}

// ContainsNUL reports whether text still holds a NUL byte after [Decode].
//
// NUL is not legal content in any schema or source file codefit reads, and the
// one thing that routinely puts it there is a UTF-16 (or UTF-32) file saved
// with no byte-order mark for Decode to act on. This is a POSITIVE observation
// about the bytes in hand, not an inference about which encoding produced them:
// the caller may say "codefit could not read this as text" — an honest,
// checkable claim — without ever asserting what the file actually was.
func ContainsNUL(text []byte) bool { return bytes.IndexByte(text, 0x00) >= 0 }

// decodeUTF16 converts UTF-16 code units to UTF-8. A trailing odd byte is
// dropped (see Decode's boundaries).
func decodeUTF16(b []byte, littleEndian bool) []byte {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if littleEndian {
			units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
		} else {
			units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
		}
	}
	return []byte(string(utf16.Decode(units)))
}
