package download

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
	SkipReasonUnrecognized   = "unrecognized url format"
)

// QueueStatus is the lifecycle state of a queue row. The SQL CHECK
// constraint remains the defense in depth; this type catches typos at
// compile time on the Go side.
type QueueStatus string

const (
	QueueStatusPending    QueueStatus = "pending"
	QueueStatusProcessing QueueStatus = "processing"
	QueueStatusFailed     QueueStatus = "failed"
)

// maxAttempts is hardcoded amount of times to retry a download
const maxAttempts = 3

// isActionable reports whether an entry will be picked up by Next.
func isActionable(e QueueEntry) bool {
	return e.Status == QueueStatusPending
}

type QueueEntry struct {
	ID       int
	SourceID string
	Status   QueueStatus
	Attempts int
	ErrorMsg string
}

// Reporter observes enqueue and queue-processing events. Its methods map
// onto the actual event sequence of Enqueue and ProcessQueue.
type Reporter interface {
	OnHybridURL(url string, forcePlaylist bool)
	OnPlaylistURL(url string)
	OnEnqueueSkipped(id string, reason string)

	OnQueueStart(totalTracks int)
	OnTrackStart(index int, total int, sourceID string)
	OnTrackSuccess(sourceID string)
	OnTrackFailure(sourceID string, err error)
}

type noOpReporter struct{}

func (noOpReporter) OnHybridURL(string, bool)        {}
func (noOpReporter) OnPlaylistURL(string)            {}
func (noOpReporter) OnEnqueueSkipped(string, string) {}

func (noOpReporter) OnQueueStart(int)              {}
func (noOpReporter) OnTrackStart(int, int, string) {}
func (noOpReporter) OnTrackSuccess(string)         {}
func (noOpReporter) OnTrackFailure(string, error)  {}

var _ Reporter = noOpReporter{}

type PlaylistSelector interface {
	SelectChannels(ctx context.Context, channels []string) ([]string, error)
}

type noOpSelector struct{}

var _ PlaylistSelector = noOpSelector{}

func (noOpSelector) SelectChannels(ctx context.Context, channels []string) ([]string, error) {
	return channels, nil
}

var (
	defaultNoOpReporter = noOpReporter{}
	defaultNoOpSelector = noOpSelector{}
)

type TrackChecker interface {
	ExistsBySourceID(ctx context.Context, id string) (bool, error)
}

// DirReader lists file names in a directory. Error contract: not-exist
// failures satisfy errors.Is(err, fs.ErrNotExist) via wrapped stdlib
// errors; callers decide whether absence is fatal.
type DirReader interface {
	ReadDir(path string) ([]string, error)
}

type QueueRepo interface {
	All(ctx context.Context) ([]QueueEntry, error)
	Enqueue(ctx context.Context, sourceID string) (bool, error)
	ResetPending(ctx context.Context, maxAttempts int) error
	Next(ctx context.Context) (QueueEntry, bool, error)
	Start(ctx context.Context, id int) error
	Complete(ctx context.Context, id int) error
	Fail(ctx context.Context, id int, status string, err error) error
	RetryByID(ctx context.Context, id int) (bool, error)
	RetryHardFailed(ctx context.Context, maxAttempts int) (int64, error)
}

// StagingDirProvider supplies the staging directory root. Satisfied by
// fs.AppPaths.
type StagingDirProvider interface {
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
	StagingDirs  StagingDirProvider
}

type Service struct {
	repo         QueueRepo
	trackChecker TrackChecker
	dirReader    DirReader
	transactor   Transactor
	stagingDirs  StagingDirProvider
}

func NewService(cfg ServiceConfig) *Service {
	return &Service{
		repo:         cfg.QueueRepo,
		trackChecker: cfg.TrackChecker,
		dirReader:    cfg.DirReader,
		transactor:   cfg.Transactor,
		stagingDirs:  cfg.StagingDirs,
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
	Reporter      Reporter
	Selector      PlaylistSelector
	ForcePlaylist bool
}

func (s *Service) Enqueue(ctx context.Context, params EnqueueParams) (EnqueueResult, error) {
	reporter := params.Reporter
	if reporter == nil {
		reporter = defaultNoOpReporter
	}

	selector := params.Selector
	if selector == nil {
		selector = defaultNoOpSelector
	}

	var result EnqueueResult
	seenIDs := make(map[string]string)

	stagedFiles, err := s.dirReader.ReadDir(s.stagingDirs.StagingDir())
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
		if err != nil || p.Type == URLTypeUnknown {
			reporter.OnEnqueueSkipped(u, SkipReasonUnrecognized)
			result.Failed++
			continue
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
		result.Failed += len(uniqueIDs)
		return result, fmt.Errorf("enqueue downloads: %w", txErr)
	}

	result.Enqueued += txEnqueued
	result.Skipped += txSkipped

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

// RetryResult reports how many queue entries were re-armed.
type RetryResult struct {
	Retried int
}

// RetryFailed re-arms failed entries for another download attempt. Explicit
// IDs target individual entries regardless of attempt count; without IDs,
// only entries that exhausted their attempts are reset.
func (s *Service) RetryFailed(ctx context.Context, ids []int) (RetryResult, error) {
	if len(ids) == 0 {
		n, err := s.repo.RetryHardFailed(ctx, maxAttempts)
		if err != nil {
			return RetryResult{}, fmt.Errorf("retry hard-failed downloads: %w", err)
		}

		return RetryResult{Retried: int(n)}, nil
	}

	var result RetryResult

	for _, id := range ids {
		ok, err := s.repo.RetryByID(ctx, id)
		if err != nil {
			return result, fmt.Errorf("retry download %d: %w", id, err)
		}

		if ok {
			result.Retried++
		}
	}

	return result, nil
}

// QueueRunConfig holds per-run settings for ProcessQueue.
type QueueRunConfig struct {
	MinSleep    string
	MaxSleep    string
	CookiesFile string
}

func (s *Service) ProcessQueue(ctx context.Context, run QueueRunConfig, reporter Reporter) (ProcessResult, error) {
	if reporter == nil {
		reporter = defaultNoOpReporter
	}

	var result ProcessResult
	targetPath := filepath.Join(s.stagingDirs.StagingDir(), "%(id)s.opus")

	if err := s.repo.ResetPending(ctx, maxAttempts); err != nil {
		return result, fmt.Errorf("reset interrupted downloads: %w", err)
	}

	current := 0
	total := 0

	pending, err := s.repo.All(ctx)
	if err != nil {
		return result, fmt.Errorf("list queue entries: %w", err)
	}

	for _, e := range pending {
		if isActionable(e) {
			total++
		}
	}

	reporter.OnQueueStart(total)

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		entry, ok, err := s.repo.Next(ctx)
		if err != nil {
			return result, fmt.Errorf("read next queue entry: %w", err)
		}
		if !ok {
			break
		}
		current++

		reporter.OnTrackStart(current, total, entry.SourceID)

		cfg := Config{
			AbsoluteOutputPath: targetPath,
			URL:                entry.SourceID,
			MinSleep:           run.MinSleep,
			MaxSleep:           run.MaxSleep,
			CookiesFile:        run.CookiesFile,
		}

		if err := s.repo.Start(ctx, entry.ID); err != nil {
			return result, fmt.Errorf("start queue entry %d: %w", entry.ID, err)
		}

		if err := Download(ctx, cfg); err != nil {
			// Cancelled run: ResetPending re-arms the row on the next run;
			// don't burn an attempt or record a bogus failure.
			if ctx.Err() != nil {
				return result, ctx.Err()
			}

			reporter.OnTrackFailure(entry.SourceID, err)
			result.Failed++

			nextStatus := QueueStatusPending
			if isTerminalError(err) || entry.Attempts >= maxAttempts {
				nextStatus = QueueStatusFailed
			}

			if err = s.repo.Fail(ctx, entry.ID, string(nextStatus), err); err != nil {
				return result, fmt.Errorf("fail queue entry %d: %w", entry.ID, err)
			}

			continue
		}

		if err := s.repo.Complete(ctx, entry.ID); err != nil {
			return result, fmt.Errorf("complete queue entry %d: %w", entry.ID, err)
		}

		reporter.OnTrackSuccess(entry.SourceID)
		result.Success++
	}

	return result, nil
}
