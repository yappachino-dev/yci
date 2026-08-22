package library

import (
	"errors"
	"testing"
)

func TestChannelServiceRoundTrip(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	artist, err := env.artists.Create(ctx, "", "", "An Artist")
	if err != nil {
		t.Fatal(err)
	}

	if err := env.channels.Link(ctx, artist.ID, "Some Channel"); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := env.channels.Link(ctx, artist.ID, "Some Channel"); !errors.Is(err, ErrChannelAlreadyLinked) {
		t.Errorf("duplicate link err = %v, want ErrChannelAlreadyLinked", err)
	}

	resolved, ok, err := env.channels.ResolveArtist(ctx, "Some Channel")
	if err != nil || !ok || resolved != artist.ID {
		t.Errorf("resolve = (%q, %t, %v), want (%q, true, nil)", resolved, ok, err, artist.ID)
	}

	all, err := env.channels.All(ctx)
	if err != nil || len(all) != 1 {
		t.Errorf("all = %+v (err %v), want one channel", all, err)
	}

	if err := env.channels.Unlink(ctx, artist.ID, "Some Channel"); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if err := env.channels.Unlink(ctx, artist.ID, "Some Channel"); !errors.Is(err, ErrChannelNotLinked) {
		t.Errorf("double unlink err = %v, want ErrChannelNotLinked", err)
	}
	if _, ok, _ := env.channels.ResolveArtist(ctx, "Some Channel"); ok {
		t.Error("channel still resolves after unlink")
	}
}
