package git

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	major, minor, patch := Version()

	// git2go v35 targets libgit2 1.9+; the runtime version must satisfy the
	// same lower bound the compile-time guard enforces.
	if major < 1 || (major == 1 && minor < 9) {
		t.Fatalf("unexpected libgit2 version %d.%d.%d, want >= 1.9.0", major, minor, patch)
	}
	if patch < 0 {
		t.Fatalf("negative patch version: %d", patch)
	}
}

func TestVersionString(t *testing.T) {
	major, minor, patch := Version()
	vs := VersionString()

	if vs == "" {
		t.Fatal("VersionString() returned empty string")
	}
	// Must start with the numeric major.minor.patch triplet.
	prefix := ""
	{
		// build "major.minor.patch" without importing fmt in the test
		var b strings.Builder
		b.WriteString(itoaTest(major))
		b.WriteByte('.')
		b.WriteString(itoaTest(minor))
		b.WriteByte('.')
		b.WriteString(itoaTest(patch))
		prefix = b.String()
	}
	if !strings.HasPrefix(vs, prefix) {
		t.Fatalf("VersionString() = %q, want prefix %q", vs, prefix)
	}

	// If a prerelease tag is present it must be reflected in the string.
	if pre := Prerelease(); pre != "" && !strings.Contains(vs, pre) {
		t.Fatalf("VersionString() = %q does not contain prerelease %q", vs, pre)
	}
}

// itoaTest is a tiny non-negative int formatter to avoid extra imports.
func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
