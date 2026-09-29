package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/yappachino-dev/yci/internal/library"
)

// TestTokenPromptAcceptsTypedSelection is the verification that was open for
// several sessions: can a person actually type a token number into the
// prompt, submit it, and get a selection back?
//
// The earlier session found a real bug here — the prompt was fed token text
// where it parsed numbers — but the fix was only ever verified at the parsing
// helper, because the form could not be driven. This drives the form.
//
// The form is built by newForm, which reads the command's input. That is why
// this is possible at all: huh.NewForm reaches for os.Stdin directly.
func TestTokenPromptAcceptsTypedSelection(t *testing.T) {
	c := newTestContainer(t)
	seedTracks(t, c, "BRAIN - Mori Calliope (cover)", "KICK BACK / 鷹嶺ルイ(cover)")

	session := newTTYCommand(t, c)

	suggested := library.DeriveFragment("BRAIN - Mori Calliope (cover)",
		[]string{"BRAIN - Mori Calliope (cover)", "KICK BACK / 鷹嶺ルイ(cover)"})

	// tokens[0]=BRAIN, [1]=Mori, [2]=Calliope, [3]=cover, [4]=KICK,
	// [5]=BACK, [6]=鷹嶺ルイ. The derived selection is the performer.
	if len(suggested.Tokens) == 0 {
		t.Fatalf("derivation produced no tokens for %v", suggested)
	}

	wait := await(func() error {
		_, _, err := pickFragmentTokens(session.cmd, "BRAIN - Mori Calliope (cover)", suggested,
			[]string{"BRAIN - Mori Calliope (cover)", "KICK BACK / 鷹嶺ルイ(cover)"},
			library.MatchAll)
		return err
	})

	// The numbered list must be on screen before anything is typed, or the
	// numbers below refer to nothing.
	session.waitFor(t, "Token numbers to match on", 5*time.Second)
	session.contains(t, "BRAIN")

	// Type the two token numbers the derivation chose, then submit.
	session.typeKeys(t, suggestedIndicesString(suggested.Tokens,
		library.Tokenize("BRAIN - Mori Calliope (cover)")))
	session.typeKeys(t, keyEnter)

	if err := wait(); err != nil {
		t.Fatalf("token prompt returned an error: %v\n--- rendered ---\n%s", err, session.output())
	}

	// Reaching here means the whole path worked: the form decoded real
	// keypresses from a real terminal, parsed them as numbers, and returned.
	if !strings.Contains(session.output(), "matches") {
		t.Errorf("expected a match count in the output\n--- rendered ---\n%s", session.output())
	}
}

// TestTokenPromptAcceptsEmptyInputAsTheSuggestion covers the specific bug
// found earlier: pressing enter with nothing typed must fall back to the
// derived indices. It returned the token *text* where the parser expected a
// number, and every user pressing enter got an error.
func TestTokenPromptAcceptsEmptyInputAsTheSuggestion(t *testing.T) {
	c := newTestContainer(t)
	seedTracks(t, c, "BRAIN - Mori Calliope (cover)", "KICK BACK / 鷹嶺ルイ(cover)")

	session := newTTYCommand(t, c)

	corpus := []string{"BRAIN - Mori Calliope (cover)", "KICK BACK / 鷹嶺ルイ(cover)"}
	suggested := library.DeriveFragment(corpus[0], corpus)

	var got []string
	wait := await(func() error {
		var err error
		got, _, err = pickFragmentTokens(session.cmd, corpus[0], suggested, corpus, library.MatchAll)
		return err
	})

	session.waitFor(t, "Token numbers to match on", 5*time.Second)

	// Nothing typed at all: just submit.
	session.typeKeys(t, keyEnter)

	if err := wait(); err != nil {
		t.Fatalf("accepting the suggestion by pressing enter failed: %v\n--- rendered ---\n%s",
			err, session.output())
	}

	if len(got) == 0 {
		t.Fatal("empty input produced no selection")
	}
	if len(got) != len(suggested.Tokens) {
		t.Errorf("selected %v, want the suggested %v", got, suggested.Tokens)
	}
}

// TestTokenPromptAcceptsModeWordThenSelection checks that typing AND is
// treated as a mode change and not as a token number, so the wizard is
// actually usable in the way its description promises.
func TestTokenPromptAcceptsModeWordThenSelection(t *testing.T) {
	c := newTestContainer(t)
	seedTracks(t, c, "星街すいせい x Mori (cover)", "completely unrelated synthwave")

	session := newTTYCommand(t, c)

	corpus := []string{"星街すいせい x Mori (cover)", "completely unrelated synthwave"}
	suggested := library.DeriveFragment(corpus[0], corpus)

	var (
		got  []string
		mode library.MatchMode
	)
	wait := await(func() error {
		var err error
		got, mode, err = pickFragmentTokens(session.cmd, corpus[0], suggested, corpus, library.MatchAll)
		return err
	})

	session.waitFor(t, "Token numbers to match on", 5*time.Second)

	// Switch the mode, which returns to the same prompt.
	session.typeKeys(t, "ANY")
	session.typeKeys(t, keyEnter)
	// Then choose the single token "Mori".
	session.waitFor(t, "Matching mode: any", 5*time.Second)
	session.typeKeys(t, "3")
	session.typeKeys(t, keyEnter)

	if err := wait(); err != nil {
		t.Fatalf("mode word was not accepted: %v\n--- rendered ---\n%s", err, session.output())
	}

	if mode != library.MatchAny {
		t.Errorf("mode = %q, want %q", mode, library.MatchAny)
	}
	if len(got) == 0 {
		t.Fatal("no tokens selected after the mode change")
	}
}

// suggestedIndicesString renders the suggested selection the way the prompt
// shows it, so the test types the same digits a user would.
func suggestedIndicesString(suggested, all []string) string {
	return formatIndices(suggestedIndices(suggested, all))
}
