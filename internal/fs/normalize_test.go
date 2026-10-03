package fs

import (
	"golang.org/x/text/unicode/norm"
	"os"
	"path/filepath"
	"testing"
)

// The two spellings that disagree. Derived rather than written as literals
// because a source file cannot be relied on to keep a decomposed sequence
// intact.
var (
	nfcName = norm.NFC.String("ネヴァモア")
	nfdName = norm.NFD.String(nfcName)
)

func TestAlternateFormFlipsBothWays(t *testing.T) {
	if norm.NFC.IsNormalString(nfdName) {
		t.Fatal("the NFD fixture is already NFC")
	}

	if got := AlternateForm(nfdName); got != nfcName {
		t.Errorf("AlternateForm(NFD) = %q, want NFC %q", got, nfcName)
	}

	if got := AlternateForm(nfcName); got != nfdName {
		t.Errorf("AlternateForm(NFC) = %q, want NFD %q", got, nfdName)
	}

	// ASCII is unaffected, so the helper is a no-op where there is nothing
	// to disagree about.
	if got := AlternateForm("plain/ascii-name.opus"); got != "plain/ascii-name.opus" {
		t.Errorf("AlternateForm changed an ASCII path: %q", got)
	}
}

func TestResolveExistingFindsTheOtherForm(t *testing.T) {
	dir := t.TempDir()

	// The file is written NFC; the caller holds the NFD spelling.
	onDisk := filepath.Join(dir, nfcName+".opus")
	if err := os.WriteFile(onDisk, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	held := filepath.Join(dir, nfdName+".opus")
	if _, err := os.Stat(held); err == nil {
		t.Fatal("the NFD spelling resolved, so the fixture proves nothing")
	}

	got, err := ResolveExisting(held)
	if err != nil {
		t.Fatalf("ResolveExisting: %v", err)
	}

	if got != onDisk {
		t.Errorf("resolved %q, want the on-disk NFC path %q", got, onDisk)
	}
}

func TestResolveExistingPassesThroughWhenExact(t *testing.T) {
	dir := t.TempDir()
	exact := filepath.Join(dir, nfcName+".opus")

	if err := os.WriteFile(exact, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ResolveExisting(exact)
	if err != nil {
		t.Fatalf("ResolveExisting: %v", err)
	}

	if got != exact {
		t.Errorf("resolved %q, want the exact path %q", got, exact)
	}
}

func TestResolveExistingReportsMissingAsNotExist(t *testing.T) {
	_, err := ResolveExisting(filepath.Join(t.TempDir(), "nothing.opus"))
	if !os.IsNotExist(err) {
		t.Fatalf("got %v, want a not-exist error", err)
	}
}
