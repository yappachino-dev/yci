package download

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yappachino-dev/yci/internal/fs"
)

const (
	SkipReasonStaged         = "already in staging directory"
	SkipReasonExists         = "already exists in database"
	SkipReasonDuplicate      = "duplicate in current request"
	SkipReasonAlreadyInQueue = "already in download queue"
	SkipReasonUnrecognized   = "unrecognized url format"
	SkipReasonRetired        = "deleted from the library"
)

// RetirementChecker reports whether a video was deliberately removed from
// the library. It is a separate seam from TrackChecker because the answers
// mean opposite things: a track that exists is waiting to be skipped, and a
// retired video is one the user asked not to see again.
type RetirementChecker interface {
	IsRetired(ctx context.Context, sourceID string) (bool, error)
}

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
	SelectChannels(ctx context.Context, channels []ChannelRef) ([]ChannelRef, error)
}

type noOpSelector struct{}

var _ PlaylistSelector = noOpSelector{}

func (noOpSelector) SelectChannels(ctx context.Context, channels []ChannelRef) ([]ChannelRef, error) {
	return channels, nil
}

var (
	defaultNoOpReporter = noOpReporter{}
	defaultNoOpSelector = noOpSelector{}
)

type TrackChecker interface {
	ExistsBySourceID(ctx context.Context, id string) (bool, error)
}

// DirReader is fs.DirReader, aliased so existing references keep working. The
// contract lives in internal/fs beside its implementation.
type DirReader = fs.DirReader

type QueueRepo interface {
	All(ctx context.Context) ([]QueueEntry, error)
	Enqueue(ctx context.Context, sourceID string) (bool, error)
	ResetPending(ctx context.Context, maxAttempts int) error
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

// Downloader fetches one track.
//
// It exists so the queue state machine can be tested. Everything else on that
// path — the queue repository, the reporter, the transactor, the directory
// reader — was already an interface, and leaving this one as a package-level
// function calling exec made the whole loop unreachable without a network and
// a toolchain, even though the logic being tested is pure: attempt counting,
// retry versus terminal classification, the reporter sequence, and the
// cancellation path that must not burn an attempt.
type Downloader interface {
	// Download returns what yt-dlp reported: the only moment the immutable
	// channel ID exists, since the file later carries only the artist's tag.
	Download(ctx context.Context, cfg Config) (VideoMetadata, error)
}

// ChannelRecorder notes which channel a downloaded video came from, keyed by
// video ID so import can join without reading the tag.
//
// Optional: a caller that does not supply one still downloads, and import
// falls back to matching the artist name as it always did.
type ChannelRecorder interface {
	RecordVideoChannel(ctx context.Context, videoID, channelID string) error
}

// PlaylistFetcher resolves a playlist URL to its channels and videos, for the
// same reason as Downloader.
type PlaylistFetcher interface {
	Fetch(ctx context.Context, url string) (PlaylistMetadata, error)
}

type ServiceConfig struct {
	QueueRepo       QueueRepo
	TrackChecker    TrackChecker
	DirReader       DirReader
	Transactor      Transactor
	StagingDirs     StagingDirProvider
	Downloader      Downloader
	PlaylistFetcher PlaylistFetcher
	Channels        ChannelRecorder
}

type Service struct {
	repo            QueueRepo
	trackChecker    TrackChecker
	dirReader       DirReader
	transactor      Transactor
	stagingDirs     StagingDirProvider
	downloader      Downloader
	playlistFetcher PlaylistFetcher
	channels        ChannelRecorder
}

// ytDlp is the real implementation of both seams: the two places this
// package shells out to another program.
type ytDlp struct {
	// out and errOut receive yt-dlp's output. Defaulting to the process
	// streams is correct for a CLI driving a real download; a caller that
	// needs them captured supplies its own.
	out, errOut io.Writer
}

func NewService(cfg ServiceConfig) *Service {
	dl := cfg.Downloader
	if dl == nil {
		dl = ytDlp{out: os.Stdout, errOut: os.Stderr}
	}

	fetcher := cfg.PlaylistFetcher
	if fetcher == nil {
		fetcher = ytDlp{out: os.Stdout, errOut: os.Stderr}
	}

	return &Service{
		channels:        cfg.Channels,
		repo:            cfg.QueueRepo,
		trackChecker:    cfg.TrackChecker,
		dirReader:       cfg.DirReader,
		transactor:      cfg.Transactor,
		stagingDirs:     cfg.StagingDirs,
		downloader:      dl,
		playlistFetcher: fetcher,
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
	// Retirement is optional. Without it a video the user deleted is
	// downloaded again the next time a watched playlist offers it.
	Retirement RetirementChecker
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

		if params.Retirement != nil {
			retired, err := params.Retirement.IsRetired(ctx, id)
			if err != nil {
				return fmt.Errorf("check retired source %q: %w", id, err)
			}

			if retired {
				reporter.OnEnqueueSkipped(id, SkipReasonRetired)
				seenIDs[id] = SkipReasonRetired
				result.Skipped++
				return nil
			}
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

			if err := s.processPlaylist(ctx, p.CleanURL(true), selector, addIDIfUnique); err != nil {
				return result, err
			}
		case URLTypeHybrid:
			reporter.OnHybridURL(u, params.ForcePlaylist)

			if params.ForcePlaylist {
				if err := s.processPlaylist(ctx, p.CleanURL(true), selector, addIDIfUnique); err != nil {
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

// FetchPlaylist resolves a playlist URL without enqueuing anything. It backs
// the source commands, which need the channel list to store a selection, and
// would otherwise enqueue every video just to look at it.
func (s *Service) FetchPlaylist(ctx context.Context, url string) (PlaylistMetadata, error) {
	return s.playlistFetcher.Fetch(ctx, url)
}

func (s *Service) processPlaylist(ctx context.Context, url string, selector PlaylistSelector, addFn func(string) error) error {
	output, err := s.playlistFetcher.Fetch(ctx, url)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	channels, err := selector.SelectChannels(ctx, output.Channels)
	if err != nil {
		return fmt.Errorf("select channels: %w", err)
	}

	channelsSet := make(map[string]struct{}, len(channels)*2)
	for _, ch := range channels {
		if ch.ID != "" {
			channelsSet[ch.ID] = struct{}{}
		}

		channelsSet[ch.Name] = struct{}{}
	}

	for _, video := range output.Videos {
		_, byID := channelsSet[video.ChannelID]
		_, byName := channelsSet[video.Channel]

		// Either half matching is enough. A selection saved from a
		// listing that reported IDs must keep working if the same
		// playlist later stops reporting them, and vice versa.
		if (video.ChannelID == "" || !byID) && !byName {
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

// recordChannel stores the download's channel before the queue row is
// completed, because Complete deletes it and this is the only durable trace
// of where the video came from.
//
// Best effort by design: losing it degrades import to the name-based path it
// used before, whereas failing the download over a bookkeeping row would lose
// the audio that was just fetched.
func (s *Service) recordChannel(ctx context.Context, entry QueueEntry, meta VideoMetadata) error {
	if s.channels == nil || meta.ChannelID == "" {
		return nil
	}

	videoID := entry.SourceID
	if meta.ID != "" {
		videoID = meta.ID
	}

	if err := s.channels.RecordVideoChannel(ctx, videoID, meta.ChannelID); err != nil {
		return nil
	}

	return nil
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

	// The work list is fixed here and walked once. A cursor that re-reads
	// the queue would re-pick a track that a transient failure had just
	// returned to pending, spending its whole retry budget in one run.
	work := make([]QueueEntry, 0, len(pending))
	for _, e := range pending {
		if isActionable(e) {
			work = append(work, e)
		}
	}
	total = len(work)

	reporter.OnQueueStart(total)

	for _, entry := range work {
		if err := ctx.Err(); err != nil {
			return result, err
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

		meta, err := s.downloader.Download(ctx, cfg)
		if err != nil {
			// Cancelled: leave the row processing and record nothing, since
			// the write would need the cancelled context. The next run's
			// ResetPending re-arms it and returns the attempt it never spent.
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

		if err := s.recordChannel(ctx, entry, meta); err != nil {
			return result, err
		}

		if err := s.repo.Complete(ctx, entry.ID); err != nil {
			return result, fmt.Errorf("complete queue entry %d: %w", entry.ID, err)
		}

		reporter.OnTrackSuccess(entry.SourceID)
		result.Success++
	}

	return result, nil
}
