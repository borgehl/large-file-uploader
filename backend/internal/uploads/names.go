package uploads

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxNameBytes = 200

// SafeName turns a client-supplied file name into something safe to use as a
// single path element. It never returns "", ".", "..", or anything containing
// a separator, control character or NUL. Client names are only ever used for
// display and for the final file name inside a server-generated directory.
func SafeName(in string) string {
	in = strings.ReplaceAll(in, "\\", "/")
	in = filepath.Base(in) // last path element; "" -> "."
	var b strings.Builder
	for _, r := range in {
		switch {
		case r == utf8.RuneError, r == 0, unicode.IsControl(r), r == '/', r == '\\', r == ':', r == '*', r == '?', r == '"', r == '<', r == '>', r == '|':
			b.WriteByte('_')
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '.', r == '-', r == '_', r == ' ', r == '(', r == ')', r == '+', r == ',', r == '[', r == ']', r == '@', r == '#', r == '&', r == '=', r == '~':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), " .")
	if out == "" {
		return "file"
	}
	if len(out) > maxNameBytes {
		ext := filepath.Ext(out)
		if len(ext) > 20 {
			ext = ""
		}
		stem := out[:maxNameBytes-len(ext)]
		for !utf8.ValidString(stem) {
			stem = stem[:len(stem)-1]
		}
		out = stem + ext
	}
	return out
}
