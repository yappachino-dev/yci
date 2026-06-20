package downloader

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	SkipReasonStaged         = "already in staging directory"
	SkipReasonExists         = "already exists in database"
	SkipReasonDuplicate      = "duplicate in current request"
	SkipReasonAlreadyInQueue = "already in download queue"
)

type EnqueueReporter interface {
	OnHybridURL(url string, forcePlaylist bool)
	OnPlaylistURL(url string)
	OnEnqueueSkipped(id string, reason string)
}

type ProgressReporter interface {
	OnQueueStart(totalTracks int)
	OnTrackStart(index int, total int, sourceID string)
	OnTrackSuccess(sourceID string)
	OnTrackFailure(sourceID string, err error)
}

type noOpReporter struct{}

func (r *noOpReporter) OnHybridURL(url string, forcePlaylist bool) {}
func (r *noOpReporter) OnPlaylistURL(url string)                   {}
func (r *noOpReporter) OnEnqueueSkipped(id string, reason string)  {}

func (r *noOpReporter) OnQueueStart(totalTracks int)                  {}
func (r *noOpReporter) OnTrackStart(index int, total int, url string) {}
func (r *noOpReporter) OnTrackSkipped(sourceID string, reason string) {}
func (r *noOpReporter) OnTrackSuccess(url string)                     {}
func (r *noOpReporter) OnTrackFailure(url string, err error)          {}

type PlaylistSelector interface {
	SelectChannels(ctx context.Context, channels []string) ([]string, error)
}

type noOpSelector struct{}

func (s *noOpSelector) SelectChannels(ctx context.Context, channels []string) ([]string, error) {
	return channels, nil
}

var (
	defaultNoOpRepoter  = &noOpReporter{}
	defaultNoOpSelector = &noOpSelector{}
)

type TrackChecker interface {
	ExistsBySourceID(ctx context.Context, id string) (bool, error)
}

type DirReader interface {
	ReadDir(path string) ([]string, error)
}

type QueueEntry struct {
	ID       int
	SourceID string
	Status   string
	Attempts int
	ErrorMsg string
}

type QueueRepo interface {
	All(ctx context.Context) ([]QueueEntry, error)
	Enqueue(ctx context.Context, sourceID string) (bool, error)
	QueueLength(ctx context.Context) (int, error)
	ResetPending(ctx context.Context) error
	NextPending(ctx context.Context) (QueueEntry, bool, error)
	Start(ctx context.Context, id int) error
	Complete(ctx context.Context, id int) error
	Fail(ctx context.Context, id int, err error) error
}

type StagingConfig interface {
	StagingDir() string
}

type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type ServiceConfig struct {
	QueueRepo    QueueRepo
	TrackChecker TrackChecker
	DirReader    DirReader
	Transactor   Transactor
	Config       StagingConfig
}

type Service struct {
	repo         QueueRepo
	trackChecker TrackChecker
	dirReader    DirReader
	transactor   Transactor
	config       StagingConfig
}

func NewService(cfg ServiceConfig) *Service {
	return &Service{
		repo:         cfg.QueueRepo,
		trackChecker: cfg.TrackChecker,
		dirReader:    cfg.DirReader,
		transactor:   cfg.Transactor,
		config:       cfg.Config,
	}
}

type EnqueueResult struct {
	Enqueued int
	Skipped  int
	Failed   int
}

func (s *Service) All(ctx context.Context) ([]QueueEntry, error) {
	q, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all queue entries: %w", err)
	}

	return q, nil
}

type EnqueueParams struct {
	URLs          []string
	Reporter      EnqueueReporter
	Selector      PlaylistSelector
	ForcePlaylist bool
}

func (s *Service) Enqueue(ctx context.Context, params EnqueueParams) (EnqueueResult, error) {
	reporter := params.Reporter
	if reporter == nil {
		reporter = defaultNoOpRepoter
	}

	selector := params.Selector
	if selector == nil {
		selector = defaultNoOpSelector
	}

	var result EnqueueResult
	seenIDs := make(map[string]string)

	stagedFiles, err := s.dirReader.ReadDir(s.config.StagingDir())
	if err != nil {
		return result, fmt.Errorf("read staged dir: %w", err)
	}

	for _, f := range stagedFiles {
		seenIDs[strings.TrimSuffix(f, ".opus")] = SkipReasonStaged
	}

	uniqueIDs := make([]string, 0, len(params.URLs))

	addIDIfUnique := func(id string) error {
		if reason, ok := seenIDs[id]; ok {
			reporter.OnEnqueueSkipped(id, reason)
			result.Skipped++
			return nil
		}

		seenIDs[id] = SkipReasonDuplicate

		exists, err := s.trackChecker.ExistsBySourceID(ctx, id)
		if err != nil {
			return fmt.Errorf("check track existence %q: %w", id, err)
		}

		if exists {
			reporter.OnEnqueueSkipped(id, SkipReasonExists)
			seenIDs[id] = SkipReasonExists
			result.Skipped++
			return nil
		}

		uniqueIDs = append(uniqueIDs, id)
		return nil
	}

	for _, u := range params.URLs {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		p, err := ParseURL(u)
		if err != nil {
			return result, err
		}

		switch p.Type {
		case URLTypeVideo:
			if err := addIDIfUnique(p.VideoID); err != nil {
				return result, err
			}
		case URLTypePlaylist:
			reporter.OnPlaylistURL(u)

			if err := processPlaylist(ctx, p.CleanURL(true), selector, addIDIfUnique); err != nil {
				return result, err
			}
		case URLTypeHybrid:
			reporter.OnHybridURL(u, params.ForcePlaylist)

			if params.ForcePlaylist {
				if err := processPlaylist(ctx, p.CleanURL(true), selector, addIDIfUnique); err != nil {
					return result, err
				}

				break
			}

			if err := addIDIfUnique(p.VideoID); err != nil {
				return result, err
			}
		}
	}

	if len(uniqueIDs) == 0 {
		return result, nil
	}

	var txEnqueued, txSkipped int

	txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		for _, id := range uniqueIDs {
			enqueued, err := s.repo.Enqueue(txCtx, id)
			if err != nil {
				return err
			}

			if enqueued {
				txEnqueued++
			} else {
				reporter.OnEnqueueSkipped(id, SkipReasonAlreadyInQueue)
				txSkipped++
			}
		}

		return nil
	})

	if txErr != nil {
		result.Failed = len(uniqueIDs)
		return result, fmt.Errorf("enqueue downloads: %w", txErr)
	}

	result.Enqueued = txEnqueued
	result.Skipped = txSkipped

	return result, nil
}

func processPlaylist(ctx context.Context, url string, selector PlaylistSelector, addFn func(string) error) error {
	output, err := parsePlaylist(ctx, url)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	channels, err := selector.SelectChannels(ctx, output.Channels)
	if err != nil {
		return fmt.Errorf("select channels: %w", err)
	}

	channelsSet := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		channelsSet[ch] = struct{}{}
	}

	for _, video := range output.Videos {
		if _, selected := channelsSet[video.Channel]; !selected {
			continue
		}

		if err := addFn(video.ID); err != nil {
			return err
		}
	}

	return nil
}

type ProcessResult struct {
	Success int
	Failed  int
}

func (s *Service) ProcessQueue(ctx context.Context, minSleep, maxSleep string, reporter ProgressReporter) (ProcessResult, error) {
	if reporter == nil {
		reporter = &noOpReporter{}
	}

	var result ProcessResult
	targetPath := filepath.Join(s.config.StagingDir(), "%(id)s.opus")

	if err := s.repo.ResetPending(ctx); err != nil {
		return result, err
	}

	current := 0
	total, err := s.repo.QueueLength(ctx)
	if err != nil {
		return result, err
	}

	reporter.OnQueueStart(total)

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		current++

		entry, ok, err := s.repo.NextPending(ctx)
		if err != nil {
			return result, err
		}
		if !ok {
			break
		}

		reporter.OnTrackStart(current, total, entry.SourceID)

		cfg := Config{
			AbsoluteOutputPath: targetPath,
			URL:                entry.SourceID,
			MinSleep:           minSleep,
			MaxSleep:           maxSleep,
		}

		if err := s.repo.Start(ctx, entry.ID); err != nil {
			return result, err
		}

		if err := Download(ctx, cfg); err != nil {
			reporter.OnTrackFailure(entry.SourceID, err)
			result.Failed++

			if err = s.repo.Fail(ctx, entry.ID, err); err != nil {
				return result, err
			}

			continue
		}

		if err := s.repo.Complete(ctx, entry.ID); err != nil {
			return result, err
		}

		reporter.OnTrackSuccess(entry.SourceID)
		result.Success++
	}

	return result, nil
}
