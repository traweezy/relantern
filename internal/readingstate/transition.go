package readingstate

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

var dismissalReasons = map[string]struct{}{
	"already_known":    {},
	"duplicate":        {},
	"irrelevant_topic": {},
	"low_quality":      {},
	"too_promotional":  {},
}

func DefaultState(storyID string, now time.Time) StoryState {
	return StoryState{
		Location:        LocationInbox,
		ReadingProgress: 0,
		StoryID:         storyID,
		TagIDs:          []string{},
		UpdatedAt:       now.UTC(),
		Version:         0,
	}
}

func Apply(current StoryState, command Command, now time.Time) (StoryState, error) {
	now = now.UTC()
	next := current
	next.TagIDs = slices.Clone(current.TagIDs)

	switch command.Action {
	case ActionMarkRead:
		next.IsRead = true
		next.ReadAt = timePointer(now)
	case ActionMarkUnread:
		markUnread(&next)
	case ActionMoveInbox:
		next.Location = LocationInbox
		next.DismissedReason = nil
		next.LaterPosition = nil
		clearSnooze(&next)
	case ActionMoveLater:
		next.Location = LocationLater
		next.DismissedReason = nil
		position := now.UnixMilli()
		next.LaterPosition = &position
		clearSnooze(&next)
	case ActionArchive:
		next.Location = LocationArchive
		next.DismissedReason = nil
		next.LaterPosition = nil
		clearSnooze(&next)
	case ActionSnooze:
		if current.Location == LocationArchive {
			return StoryState{}, fmt.Errorf("%w: archived stories must be restored before snoozing", ErrInvalid)
		}
		if command.SnoozedUntil == nil {
			return StoryState{}, fmt.Errorf("%w: snooze requires a return time", ErrInvalid)
		}
		until := command.SnoozedUntil.UTC()
		if !until.After(now) || until.After(now.Add(366*24*time.Hour)) {
			return StoryState{}, fmt.Errorf("%w: snooze return time must be within the next year", ErrInvalid)
		}
		next.SnoozedUntil = &until
		from := current.Location
		next.SnoozedFromLocation = &from
		markUnread(&next)
	case ActionUnsnooze:
		clearSnooze(&next)
		markUnread(&next)
	case ActionReturnSnoozed:
		if current.SnoozedUntil == nil || current.SnoozedFromLocation == nil || current.SnoozedUntil.After(now) {
			return StoryState{}, fmt.Errorf("%w: only snoozed stories can be returned", ErrInvalid)
		}
		clearSnooze(&next)
		markUnread(&next)
	case ActionStar:
		next.StarredAt = timePointer(now)
	case ActionUnstar:
		next.StarredAt = nil
	case ActionDismiss:
		next.Location = LocationArchive
		next.LaterPosition = nil
		clearSnooze(&next)
		if command.DismissedReason != nil {
			reason := strings.TrimSpace(*command.DismissedReason)
			if _, allowed := dismissalReasons[reason]; !allowed {
				return StoryState{}, fmt.Errorf("%w: unsupported dismissal reason", ErrInvalid)
			}
			next.DismissedReason = &reason
		} else {
			next.DismissedReason = nil
		}
	case ActionAlreadyKnown:
		next.IsRead = true
		next.ReadAt = timePointer(now)
	case ActionUpdateProgress:
		if command.ReadingProgress == nil || *command.ReadingProgress < 0 || *command.ReadingProgress > 1 {
			return StoryState{}, fmt.Errorf("%w: reading progress must be between zero and one", ErrInvalid)
		}
		if command.LastParagraphID != nil {
			paragraphID := strings.TrimSpace(*command.LastParagraphID)
			if paragraphID == "" || len(paragraphID) > 255 {
				return StoryState{}, fmt.Errorf("%w: paragraph ID must contain 1 through 255 characters", ErrInvalid)
			}
			next.LastParagraphID = &paragraphID
		}
		next.ReadingProgress = *command.ReadingProgress
	case ActionAddTag:
		if command.TagID == nil || strings.TrimSpace(*command.TagID) == "" {
			return StoryState{}, fmt.Errorf("%w: add tag requires a tag ID", ErrInvalid)
		}
		if !slices.Contains(next.TagIDs, *command.TagID) {
			next.TagIDs = append(next.TagIDs, *command.TagID)
			slices.Sort(next.TagIDs)
		}
	case ActionRemoveTag:
		if command.TagID == nil || strings.TrimSpace(*command.TagID) == "" {
			return StoryState{}, fmt.Errorf("%w: remove tag requires a tag ID", ErrInvalid)
		}
		next.TagIDs = slices.DeleteFunc(next.TagIDs, func(tagID string) bool {
			return tagID == *command.TagID
		})
	case ActionReorderLater:
		if current.Location != LocationLater || command.LaterPosition == nil || *command.LaterPosition < 0 {
			return StoryState{}, fmt.Errorf("%w: only Later stories accept a non-negative position", ErrInvalid)
		}
		position := *command.LaterPosition
		next.LaterPosition = &position
	default:
		return StoryState{}, fmt.Errorf("%w: unsupported action %q", ErrInvalid, command.Action)
	}

	next.Version = current.Version + 1
	next.UpdatedAt = now
	if err := Validate(next); err != nil {
		return StoryState{}, err
	}
	return next, nil
}

func Validate(state StoryState) error {
	if state.Location != LocationInbox && state.Location != LocationLater && state.Location != LocationArchive {
		return fmt.Errorf("%w: unsupported location", ErrInvalid)
	}
	if state.IsRead != (state.ReadAt != nil) {
		return fmt.Errorf("%w: read timestamp does not match read state", ErrInvalid)
	}
	if state.ReadingProgress < 0 || state.ReadingProgress > 1 {
		return fmt.Errorf("%w: reading progress is outside its bounds", ErrInvalid)
	}
	if (state.SnoozedUntil == nil) != (state.SnoozedFromLocation == nil) {
		return fmt.Errorf("%w: partial snooze state", ErrInvalid)
	}
	if state.SnoozedFromLocation != nil {
		if state.Location == LocationArchive || state.Location != *state.SnoozedFromLocation {
			return fmt.Errorf("%w: snooze location does not match the active location", ErrInvalid)
		}
	}
	if state.Version < 0 {
		return fmt.Errorf("%w: negative version", ErrInvalid)
	}
	return nil
}

func clearSnooze(state *StoryState) {
	state.SnoozedFromLocation = nil
	state.SnoozedUntil = nil
}

func markUnread(state *StoryState) {
	state.IsRead = false
	state.ReadAt = nil
}

func timePointer(value time.Time) *time.Time {
	return &value
}
