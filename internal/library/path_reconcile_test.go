package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// nfcName and nfdName are the same visible string in the two forms ext4
// treats as different files. Derived, not literal, so the source file cannot
// normalize one of them behind the test's back.
var (
	nfcName = norm.NFC.String("ネヴァモア")
	nfdName = norm.NFD.String(nfcName)
)

// seedTrackOnDisk writes a real file at the given library-relative path.
func seedTrackOnDisk(t *testing.T, env *libraryEnv, rel string) string {
	t.Helper()

	abs := filepath.Join(env.paths.LibraryDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(abs, []byte("x"), 0o600); err != nil {
		t.Fatalf("write %q: %v", abs, err)
	}

	return abs
}

// TestReconcilePathsRewritesOnlyWhenTheOtherFormExists is the regression
// guard for TD-44: a path stored in the form the filesystem does not use must
// be repaired, and only because the other form is provably there.
func TestReconcilePathsRewritesOnlyWhenTheOtherFormExists(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := context.Background()

	dir := "Artists/VT/Hololive/EN/Cecilia Immergreen"

	// Stored NFD, on disk NFC.
	seedTrackOnDisk(t, env, dir+"/【MV】nevermore _ "+nfcName+".opus")

	if _, err := env.tracks.Create(ctx, TrackCreateParams{
		ID:           TrackID("t-1"),
		Title:        "nevermore",
		RelativePath: TrackPath(dir + "/【MV】nevermore _ " + nfdName + ".opus"),
		Source:       SourceYouTube,
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	report, err := env.tracks.ReconcilePaths(ctx, true)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(report.Repairs) != 1 {
		t.Fatalf("got %d repairs, want 1: %+v", len(report.Repairs), report.Repairs)
	}

	if report.Missing != 0 {
		t.Errorf("a resolvable file was counted missing")
	}

	got := string(report.Repairs[0].NewPath)
	if !norm.NFC.IsNormalString(got) {
		t.Errorf("repaired path %q is still not NFC", got)
	}

	if _, err := os.Stat(filepath.Join(env.paths.LibraryDir(), filepath.FromSlash(got))); err != nil {
		t.Errorf("repaired path does not resolve to a file: %v", err)
	}
}

// TestReconcilePathsDryRunWritesNothing is the other half of the contract:
// previewing must not change what the next run sees.
func TestReconcilePathsDryRunWritesNothing(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := context.Background()

	dir := "Artists/EN"
	seedTrackOnDisk(t, env, dir+"/"+nfcName+".opus")

	stored := dir + "/" + nfdName + ".opus"
	if _, err := env.tracks.Create(ctx, TrackCreateParams{
		ID:           TrackID("t-1"),
		Title:        "x",
		RelativePath: TrackPath(stored),
		Source:       SourceYouTube,
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	if _, err := env.tracks.ReconcilePaths(ctx, true); err != nil {
		t.Fatalf("dry run: %v", err)
	}

	tracks, err := env.tracks.ByID(ctx, TrackID("t-1"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if string(tracks.RelativePath) != stored {
		t.Errorf("dry run changed the stored path to %q", tracks.RelativePath)
	}
}

func TestReconcilePathsAppliesAndIsIdempotent(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := context.Background()

	dir := "Artists/EN"
	seedTrackOnDisk(t, env, dir+"/"+nfcName+".opus")

	if _, err := env.tracks.Create(ctx, TrackCreateParams{
		ID:           TrackID("t-1"),
		Title:        "x",
		RelativePath: TrackPath(dir + "/" + nfdName + ".opus"),
		Source:       SourceYouTube,
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	if _, err := env.tracks.ReconcilePaths(ctx, false); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	tracks, err := env.tracks.ByID(ctx, TrackID("t-1"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if !norm.NFC.IsNormalString(string(tracks.RelativePath)) {
		t.Errorf("path was not applied: %q", tracks.RelativePath)
	}

	second, err := env.tracks.ReconcilePaths(ctx, false)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	if len(second.Repairs) != 0 {
		t.Errorf("second run repaired %d paths; it must converge", len(second.Repairs))
	}
}

// TestReconcilePathsLeavesGenuinelyMissingAlone: the repair must not invent a
// path for a file that is actually gone.
func TestReconcilePathsLeavesGenuinelyMissingAlone(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := context.Background()

	// Deliberately non-ASCII: for an ASCII path the alternate form is the
	// same string, so an ASCII fixture cannot catch a bogus rewrite.
	stored := "Artists/EN/" + nfdName + "-gone.opus"
	if _, err := env.tracks.Create(ctx, TrackCreateParams{
		ID:           TrackID("t-1"),
		Title:        "x",
		RelativePath: TrackPath(stored),
		Source:       SourceYouTube,
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	report, err := env.tracks.ReconcilePaths(ctx, false)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(report.Repairs) != 0 {
		t.Errorf("repaired %d paths for a file that does not exist", len(report.Repairs))
	}

	if report.Missing != 1 {
		t.Errorf("Missing = %d, want 1", report.Missing)
	}

	for _, r := range report.Repairs {
		if string(r.OldPath) != stored {
			t.Errorf("repair invented %q for a file that is absent", r.NewPath)
		}
	}

	tracks, _ := env.tracks.ByID(ctx, TrackID("t-1"))
	if string(tracks.RelativePath) != stored {
		t.Errorf("stored path changed to %q for a missing file", tracks.RelativePath)
	}
}

// TestReconcilePathsIgnoresHealthyTracks: 340 correct paths must produce no
// work, or the command is noise on every run.
func TestReconcilePathsIgnoresHealthyTracks(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := context.Background()

	rel := "Artists/EN/" + nfcName + ".opus"
	seedTrackOnDisk(t, env, rel)

	if _, err := env.tracks.Create(ctx, TrackCreateParams{
		ID: TrackID("t-1"), Title: "x", RelativePath: TrackPath(rel), Source: SourceYouTube,
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	report, err := env.tracks.ReconcilePaths(ctx, false)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if report.Checked != 1 || len(report.Repairs) != 0 || report.Missing != 0 {
		t.Errorf("healthy library produced work: %+v", report)
	}
}
