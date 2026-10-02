package download

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
)

// recordingChannels stands in for the library's channel repository.
type recordingChannels struct {
	recorded map[string]string
}

func (r *recordingChannels) RecordVideoChannel(_ context.Context, videoID, channelID string) error {
	if r.recorded == nil {
		r.recorded = map[string]string{}
	}
	r.recorded[videoID] = channelID
	return nil
}

// TestParseMetadataLine pins the parse of the line --print emits. A field
// shifting position would silently mis-attribute a download to the wrong
// channel, which is the failure this whole path exists to prevent.
func TestParseMetadataLine(t *testing.T) {
	meta, ok := parseMetadataLine("yci-video\tdQw4w9WgXcQ\tUC8rcEBzJSleTkf_-agPM20g\tIRyS Ch. hololive")
	if !ok {
		t.Fatal("parseMetadataLine = not ok, want the metadata line recognised")
	}
	if meta.ID != "dQw4w9WgXcQ" {
		t.Errorf("video id = %q, want dQw4w9WgXcQ", meta.ID)
	}
	if meta.ChannelID != "UC8rcEBzJSleTkf_-agPM20g" {
		t.Errorf("channel id = %q, want the immutable id", meta.ChannelID)
	}
	if meta.Channel != "IRyS Ch. hololive" {
		t.Errorf("channel name = %q, want the display name", meta.Channel)
	}
}

// TestParseMetadataLineIgnoresEverythingElse: stdout also carries yt-dlp's
// own output, and none of it may be mistaken for our line.
func TestParseMetadataLineIgnoresEverythingElse(t *testing.T) {
	others := []string{
		"",
		"[youtube] Extracting URL: https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"100% of 3.42MiB in 00:01",
		"yci-video\ttoo\tfew",
		"not-yci-video\ta\tUC1\tName",
		"[download] Destination: /tmp/x.opus",
	}
	for _, line := range others {
		if _, ok := parseMetadataLine(line); ok {
			t.Errorf("parseMetadataLine(%q) = ok, want it passed through untouched", line)
		}
	}
}

// TestBuildArgsAsksForTheChannelID: --print is what carries the identity, and
// --dump-json would simulate the download instead of performing it.
func TestBuildArgsAsksForTheChannelID(t *testing.T) {
	args := strings.Join(buildArgs(Config{MinSleep: "1", MaxSleep: "2"}), " ")

	if !strings.Contains(args, "--print "+metadataPrefix) {
		t.Errorf("args do not request the metadata line: %s", args)
	}
	if !strings.Contains(args, "%(channel_id)s") {
		t.Errorf("args do not ask for channel_id: %s", args)
	}
	if strings.Contains(args, "--dump-json") || strings.Contains(args, "-J") {
		t.Errorf("args use --dump-json, which simulates the download: %s", args)
	}
}

// TestProcessQueueRecordsTheChannel is the join that makes rename survivable:
// whatever a download reports is persisted before the queue row is deleted.
func TestProcessQueueRecordsTheChannel(t *testing.T) {
	const videoID = "dQw4w9WgXcQ"
	dl := &fakeDownloader{
		metadata: map[string]VideoMetadata{
			videoID: {ID: videoID, ChannelID: "UC8rcEBzJSleTkf_-agPM20g", Channel: "IRyS Ch. hololive"},
		},
	}
	channels := &recordingChannels{}

	repo := newQueueRepo(t)
	svc := NewService(ServiceConfig{
		QueueRepo:   repo,
		StagingDirs: staticStaging{StagingDirPath: t.TempDir()},
		Downloader:  dl,
		Channels:    channels,
	})

	mustEnqueue(t, repo, videoID)

	if _, err := svc.ProcessQueue(context.Background(), QueueRunConfig{}, noOpReporter{}); err != nil {
		t.Fatalf("ProcessQueue: %v", err)
	}

	if got := channels.recorded[videoID]; got != "UC8rcEBzJSleTkf_-agPM20g" {
		t.Errorf("recorded channel for %s = %q, want the immutable id", videoID, got)
	}
}

// TestProcessQueueWithoutAChannelIsNotAnError: yt-dlp does not always report
// one, and a missing identity must degrade to the old name path rather than
// fail a download that already succeeded.
func TestProcessQueueWithoutAChannelIsNotAnError(t *testing.T) {
	const videoID = "dQw4w9WgXcQ"
	dl := &fakeDownloader{}
	channels := &recordingChannels{}

	repo := newQueueRepo(t)
	svc := NewService(ServiceConfig{
		QueueRepo:   repo,
		StagingDirs: staticStaging{StagingDirPath: t.TempDir()},
		Downloader:  dl,
		Channels:    channels,
	})

	mustEnqueue(t, repo, videoID)

	res, err := svc.ProcessQueue(context.Background(), QueueRunConfig{}, noOpReporter{})
	if err != nil {
		t.Fatalf("ProcessQueue: %v", err)
	}
	if res.Success != 1 {
		t.Errorf("success = %d, want 1", res.Success)
	}
	if len(channels.recorded) != 0 {
		t.Errorf("recorded %v, want nothing when yt-dlp reported no channel", channels.recorded)
	}
}

// TestConsumeStdoutTakesOnlyOurLine exercises the real loop rather than a
// copy of it: the metadata line must be consumed, and everything else must
// reach the user unchanged.
func TestConsumeStdoutTakesOnlyOurLine(t *testing.T) {
	input := strings.Join([]string{
		"[youtube] Extracting URL: https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"yci-video\tdQw4w9WgXcQ\tUC8rcEBzJSleTkf_-agPM20g\tIRyS Ch. hololive",
		"[download] Destination: /tmp/x.opus",
		"[download] 100% of 3.42MiB in 00:01",
	}, "\n") + "\n"

	var out bytes.Buffer

	meta, err := consumeStdout(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("consumeStdout: %v", err)
	}

	if meta.ChannelID != "UC8rcEBzJSleTkf_-agPM20g" {
		t.Errorf("channel id = %q, want the immutable id", meta.ChannelID)
	}
	if strings.Contains(out.String(), metadataPrefix) {
		t.Errorf("metadata line leaked to the user: %q", out.String())
	}
	for _, want := range []string{"[youtube] Extracting URL", "[download] Destination", "100% of 3.42MiB"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("yt-dlp output %q was swallowed: %q", want, out.String())
		}
	}
}

// TestConsumeStdoutWithNoMetadataLine: yt-dlp did not report one. The download
// still succeeded, so this returns empty metadata rather than an error.
func TestConsumeStdoutWithNoMetadataLine(t *testing.T) {
	var out bytes.Buffer

	meta, err := consumeStdout(strings.NewReader("just some output\n"), &out)
	if err != nil {
		t.Fatalf("consumeStdout: %v", err)
	}
	if meta.ChannelID != "" {
		t.Errorf("channel id = %q, want empty", meta.ChannelID)
	}
	if !strings.Contains(out.String(), "just some output") {
		t.Errorf("output swallowed: %q", out.String())
	}
}

// TestMetadataTemplateMatchesTheParser guards the coupling between the --print
// template and parseMetadataLine. A template with a different field count
// parses as "not our line", so the resolver silently reports no channel and
// every backfill comes back empty with no error anywhere.
func TestMetadataTemplateMatchesTheParser(t *testing.T) {
	fields := strings.Split(metadataTemplate, "\t")
	if len(fields) != 4 {
		t.Fatalf("metadataTemplate has %d fields, parseMetadataLine requires 4: %q", len(fields), metadataTemplate)
	}
	if fields[0] != metadataPrefix {
		t.Errorf("template starts with %q, want %q", fields[0], metadataPrefix)
	}

	// A line shaped exactly like the template's output must be recognised,
	// which is the property the resolver depends on end to end.
	sample := metadataPrefix + "\tdQw4w9WgXcQ\tUC8rcEBzJSleTkf_-agPM20g\tIRyS Ch. hololive"
	if _, ok := parseMetadataLine(sample); !ok {
		t.Error("a line shaped like the template's own output was not recognised")
	}

	// And the resolver must actually print that template. Asserting on the
	// constant alone passed while the resolver used a shorter one, which made
	// every lookup return no channel and no error.
	resolverArgs := buildResolveArgs("dQw4w9WgXcQ", "")
	if !slices.Contains(resolverArgs, metadataTemplate) {
		t.Errorf("resolver does not print metadataTemplate: %v", resolverArgs)
	}
	if slices.Contains(resolverArgs, "--dump-json") {
		t.Errorf("resolver uses --dump-json, which simulates the download: %v", resolverArgs)
	}
}
