package uploads

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":             "report.pdf",
		"../../etc/passwd":       "passwd",
		"..\\..\\windows\\x.exe": "x.exe",
		"/abs/path/file.bin":     "file.bin",
		"..":                     "file",
		".":                      "file",
		"":                       "file",
		"   ":                    "file",
		".hidden":                "hidden",
		"a\x00b.txt":             "a_b.txt",
		"nul\nline.txt":          "nul_line.txt",
		"na:me*?.txt":            "na_me__.txt",
		"données été.tar.gz":     "données été.tar.gz",
		"trailing dot. ":         "trailing dot",
		"a/b/../../c":            "c",
	}
	for in, want := range cases {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeNameInvariants(t *testing.T) {
	long := strings.Repeat("é", 300) + ".tar.gz"
	inputs := []string{long, strings.Repeat("a", 1000), "x/../" + long, "\xff\xfe.txt", "../" + strings.Repeat("b", 500)}
	for _, in := range inputs {
		got := SafeName(in)
		if len(got) == 0 || len(got) > maxNameBytes {
			t.Errorf("bad length %d for %q", len(got), in)
		}
		if !utf8.ValidString(got) {
			t.Errorf("invalid utf8 for %q", in)
		}
		if got == "." || got == ".." || strings.ContainsAny(got, "/\\\x00") {
			t.Errorf("unsafe result %q for %q", got, in)
		}
	}
	if !strings.HasSuffix(SafeName(long), ".gz") {
		t.Errorf("extension lost when truncating")
	}
}
