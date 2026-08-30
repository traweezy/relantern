package readingstate

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

func TestReadingStateIndependentInvariants(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	random := rand.New(rand.NewPCG(13, 19))
	state := DefaultState("story", now)
	for index := range 2_000 {
		actions := []Action{
			ActionArchive, ActionMarkRead, ActionMarkUnread, ActionMoveInbox,
			ActionMoveLater, ActionStar, ActionUnstar, ActionUnsnooze,
		}
		action := actions[random.IntN(len(actions))]
		next, err := Apply(state, Command{Action: action}, now.Add(time.Duration(index+1)*time.Second))
		if err != nil {
			t.Fatalf("apply %s: %v", action, err)
		}
		if err := Validate(next); err != nil {
			t.Fatalf("validate after %s: %v", action, err)
		}
		if action == ActionArchive && next.StarredAt == nil && state.StarredAt != nil {
			t.Fatal("archive erased independent star state")
		}
		state = next
	}
}

func TestSnoozeRestoresUnreadPriorLocation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	state := DefaultState("story", now)
	state, _ = Apply(state, Command{Action: ActionMoveLater}, now.Add(time.Second))
	state, _ = Apply(state, Command{Action: ActionMarkRead}, now.Add(2*time.Second))
	until := now.Add(24 * time.Hour)
	state, err := Apply(state, Command{Action: ActionSnooze, SnoozedUntil: &until}, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if state.Location != LocationLater || state.IsRead || state.SnoozedFromLocation == nil {
		t.Fatalf("unexpected snoozed state: %#v", state)
	}
	state, err = Apply(state, Command{Action: ActionUnsnooze}, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if state.Location != LocationLater || state.IsRead || state.SnoozedUntil != nil {
		t.Fatalf("unexpected restored state: %#v", state)
	}
	if _, err := Apply(state, Command{Action: ActionReturnSnoozed}, now.Add(5*time.Second)); err == nil {
		t.Fatal("returning an already restored snooze succeeded")
	}
}

func TestArchivePreservesKnowledgeState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	progress := 0.6
	paragraph := "paragraph-6"
	state := DefaultState("story", now)
	state.TagIDs = []string{"tag-a"}
	state, _ = Apply(state, Command{Action: ActionStar}, now.Add(time.Second))
	state, _ = Apply(state, Command{
		Action: ActionUpdateProgress, LastParagraphID: &paragraph, ReadingProgress: &progress,
	}, now.Add(2*time.Second))
	before := state
	state, err := Apply(state, Command{Action: ActionArchive}, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if state.StarredAt == nil || state.ReadingProgress != before.ReadingProgress ||
		!reflect.DeepEqual(state.TagIDs, before.TagIDs) || state.LastParagraphID == nil {
		t.Fatalf("archive erased orthogonal knowledge state: %#v", state)
	}
}

func TestInvalidTransitionsFailClosed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	archived, _ := Apply(DefaultState("story", now), Command{Action: ActionArchive}, now)
	until := now.Add(time.Hour)
	if _, err := Apply(archived, Command{Action: ActionSnooze, SnoozedUntil: &until}, now); err == nil {
		t.Fatal("archived story snooze succeeded")
	}
	invalidProgress := 1.1
	if _, err := Apply(archived, Command{Action: ActionUpdateProgress, ReadingProgress: &invalidProgress}, now); err == nil {
		t.Fatal("out-of-range progress succeeded")
	}
}
