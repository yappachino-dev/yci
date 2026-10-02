package library

import (
	"context"
	"fmt"
)

// TitleInterpretation is the outcome of interpreting one track's raw title.
type TitleInterpretation struct {
	// Title is the display title: the raw title with decoration and
	// version markers removed. Structure is never inferred here, so this
	// value can only be less noisy than the original, never rearranged.
	Title string
	// Performer and Version are untrusted guesses, recorded as hints and
	// never written into Title. The confidence that once gated them was
	// measured to be unreliable.
	Performer string
	Version   string
}

// InterpretTitle strips decoration from the title and records the parser's
// structural guesses alongside it as hints. Stripping is unconditional;
// inference contributes nothing to the stored title.
func InterpretTitle(rawTitle string) TitleInterpretation {
	parts := ParseTitle(rawTitle)

	return TitleInterpretation{
		Title:     StripTitle(rawTitle),
		Performer: parts.Performer,
		Version:   parts.Version,
	}
}

// TitleChange records one retitling decision for reporting.
type TitleChange struct {
	ID        TrackID
	From      string
	To        string
	Performer string
	Version   string
	// Retitled distinguishes a real title change from a row that only
	// gained provenance and hints.
	Retitled bool
}

// RetitleReport summarizes a retitle pass.
type RetitleReport struct {
	Scanned int
	// Updated counts rows written, including rows that only recorded
	// provenance.
	Updated int
	// Retitled counts rows whose display title actually changed.
	Retitled int
	Changes  []TitleChange
}

// pendingTitleUpdate carries everything one row needs to be rewritten.
type pendingTitleUpdate struct {
	raw         string
	interpreted TitleInterpretation
}

// Retitle re-derives display titles, performers and versions from stored
// raw titles. Database only: no file is renamed and no tag is rewritten, so
// a re-run is free and reversible from raw_title. With dryRun the report
// describes what would change without writing.
func (s *TrackService) Retitle(ctx context.Context, dryRun bool) (RetitleReport, error) {
	var report RetitleReport

	tracks, err := s.All(ctx)
	if err != nil {
		return report, fmt.Errorf("list tracks: %w", err)
	}

	updates := make(map[TrackID]pendingTitleUpdate, len(tracks))

	for _, t := range tracks {
		report.Scanned++

		raw := t.RawTitle
		if raw == "" {
			raw = t.Title // rows predating provenance hold their title
		}

		interpreted := InterpretTitle(raw)
		if interpreted.Title == t.Title && interpreted.Performer == t.Performer &&
			interpreted.Version == t.Version && t.RawTitle != "" {
			continue
		}

		report.Updated++
		if interpreted.Title != t.Title {
			report.Retitled++
		}

		report.Changes = append(report.Changes, TitleChange{
			ID:        t.ID,
			From:      t.Title,
			To:        interpreted.Title,
			Performer: interpreted.Performer,
			Version:   interpreted.Version,
			Retitled:  interpreted.Title != t.Title,
		})
		updates[t.ID] = pendingTitleUpdate{raw: raw, interpreted: interpreted}
	}

	if dryRun || len(updates) == 0 {
		return report, nil
	}

	txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		for id, update := range updates {
			u := update.interpreted
			if err := s.repo.UpdateTitle(txCtx, id, u.Title, update.raw, u.Performer, u.Version); err != nil {
				return err
			}
		}

		return nil
	})
	if txErr != nil {
		return RetitleReport{}, fmt.Errorf("apply retitling: %w", txErr)
	}

	return report, nil
}
