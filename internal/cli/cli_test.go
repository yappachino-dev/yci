package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/config"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/library"
)

type slugID string

func TestResolveIDByPrefix(t *testing.T) {
	all := []slugID{"alpha", "alphabet", "beta"}
	two := []slugID{"alpha", "bravo"}

	tests := []struct {
		name    string
		input   string
		all     []slugID
		want    slugID
		wantErr string // substring of expected error; "" means success
	}{
		{"exact id passes through", "beta", all, "beta", ""},
		{"unique prefix resolves", "al", two, "alpha", ""},
		{
			"exact id wins even though it prefixes another",
			"alpha",
			all,
			"alpha",
			"",
		},
		{"no match", "zzz", all, "", `no widget matches ID "zzz"`},
		{
			"ambiguous prefix lists candidates",
			"a",
			all,
			"",
			`ambiguous ID prefix "a" matches 2 widgets: alpha, alphabet`,
		},
		{"empty input never matches", "", all, "", "no widget matches empty ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveIDByPrefix(tt.input, tt.all, "widget")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("resolveIDByPrefix(%q): %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("resolveIDByPrefix(%q) succeeded, want error %q", tt.input, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestIDFromArgsOrFlag(t *testing.T) {
	newCmd := func(t *testing.T) *cobra.Command {
		t.Helper()

		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("id", "", "")
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatalf("parse flags: %v", err)
		}
		return cmd
	}

	tests := []struct {
		name    string
		args    []string
		flagVal string
		flagSet bool
		want    string
		wantErr string
	}{
		{"argument wins over nothing", []string{" abc "}, "", false, "abc", ""},
		{"flag used when no argument", nil, " xyz ", true, "xyz", ""},
		{"both supplied is an error", []string{"a"}, "b", true, "", "not both"},
		{"multiple arguments are an error", []string{"a", "b"}, "", false, "", "at most one"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCmd(t)
			if tt.flagSet {
				if err := cmd.Flags().Set("id", tt.flagVal); err != nil {
					t.Fatalf("set flag: %v", err)
				}
			}

			got, err := idFromArgsOrFlag(cmd, "id", tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func openSQLite(t *testing.T) (*sql.DB, error) {
	t.Helper()

	dbConn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { dbConn.Close() })

	return dbConn, nil
}

// newTestContainer boots a real migrated database and container, mirroring
// production wiring (real schema from db/migrations, not a hand-written
// fixture).
func newTestContainer(t *testing.T) *app.Container {
	t.Helper()

	if err := db.SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}

	dbConn, err := openSQLite(t)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	ctx := context.Background()
	if err := db.RunMigrations(ctx, dbConn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := config.Config{LibraryDir: t.TempDir(), StagingDir: t.TempDir()}
	c, err := app.NewContainer(dbConn, cfg)
	if err != nil {
		t.Fatalf("new container: %v", err)
	}

	return c
}

func TestResolveEntityIDsAgainstRealSchema(t *testing.T) {
	c := newTestContainer(t)
	ctx := context.Background()

	root := library.FolderID(library.RootID)
	f1, err := c.FolderService.Create(ctx, "", root, library.FolderName("Ambient Works"))
	if err != nil {
		t.Fatalf("create folder 1: %v", err)
	}
	f2, err := c.FolderService.Create(ctx, "", root, library.FolderName("Ambient Vol 2"))
	if err != nil {
		t.Fatalf("create folder 2: %v", err)
	}
	a1, err := c.ArtistService.Create(ctx, "", f1.ID, library.ArtistName("Biosphere"))
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}

	t.Run("folder prefix resolves uniquely", func(t *testing.T) {
		got, err := resolveFolderID(ctx, c, "ambient-w")
		if err != nil {
			t.Fatalf("resolveFolderID: %v", err)
		}
		if got != f1.ID {
			t.Errorf("got %q, want %q", got, f1.ID)
		}
	})

	t.Run("folder ambiguity reports candidates", func(t *testing.T) {
		_, err := resolveFolderID(ctx, c, "ambient")
		if err == nil {
			t.Fatal("expected ambiguity error")
		}
		for _, want := range []string{string(f1.ID), string(f2.ID)} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention candidate %q", err, want)
			}
		}
	})

	t.Run("artist exact id resolves without listing", func(t *testing.T) {
		got, err := resolveArtistID(ctx, c, string(a1.ID))
		if err != nil {
			t.Fatalf("resolveArtistID: %v", err)
		}
		if got != a1.ID {
			t.Errorf("got %q, want %q", got, a1.ID)
		}
	})

	t.Run("unknown artist errors cleanly", func(t *testing.T) {
		_, err := resolveArtistID(ctx, c, "nobody")
		if err == nil || !strings.Contains(err.Error(), `no artist matches ID "nobody"`) {
			t.Errorf("err = %v, want clean no-match message", err)
		}
	})

	t.Run("track resolver works on empty table", func(t *testing.T) {
		_, err := resolveTrackID(ctx, c, "abc")
		if err == nil || !strings.Contains(err.Error(), `no track matches ID "abc"`) {
			t.Errorf("err = %v, want clean no-match message", err)
		}
	})
}

func TestMapSentinels(t *testing.T) {
	base := fmt.Errorf("artist by id %q: %w", "x", library.ErrArtistNotFound)

	msg := mapSentinels(base, map[error]string{
		library.ErrArtistNotFound: "artist does not exist",
	})
	if msg.Error() != "artist does not exist" {
		t.Errorf("mapped message = %q, want single clean line", msg)
	}

	unmapped := mapSentinels(errors.New("boom"), map[error]string{
		library.ErrArtistNotFound: "artist does not exist",
	})
	if unmapped.Error() != "boom" {
		t.Errorf("unmapped error changed: %q", unmapped)
	}
}
