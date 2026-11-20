// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/hilli/go-kef-w2/kefw2"
)

var (
	errPlayerQueueBusy         = errors.New("another queue operation is in flight")
	errPlayerQueueStale        = errors.New("the queue changed; review it before trying again")
	errPlayerQueueWrongSession = errors.New("the queue snapshot belongs to another or missing session")
	errPlayerQueueConfirmation = errors.New("clearing the queue requires confirmation")
	errPlayerQueueUncertain    = errors.New("the queue action may have been accepted; review before trying again")
	errPlayerQueueReview       = errors.New("review and acknowledge the uncertain queue action first")
)

// playerQueueClient is exclusively owned by one playerQueue. In particular,
// the caller must not change its speaker, context, or cache while it is in use.
type playerQueueClient interface {
	GetPlayQueue() (*kefw2.RowsResponse, error)
	PlayQueueIndex(int, *kefw2.ContentItem) error
	MoveQueueItem(int, int) error
	RemoveFromQueue([]int) error
	ClearPlaylist() error
	GetPlayMode() (string, error)
	SetPlayMode(string) error
}

var _ playerQueueClient = (*kefw2.AirableClient)(nil)

type playerQueueReviewKind uint8

const (
	playerQueueReviewed playerQueueReviewKind = iota
	playerQueueReviewRows
	playerQueueReviewMode
)

// playerQueue serializes all access to its client. Concurrent operations fail
// with errPlayerQueueBusy instead of being queued for a potentially old session.
type playerQueue struct {
	session     context.Context
	client      playerQueueClient
	inFlight    chan struct{}
	reviewKind  playerQueueReviewKind
	reviewReady bool
}

// The client must be newly constructed for one fixed target with
// kefw2.WithContext(session), without a shared browsing cache. Pass the session
// context, or a child of it, to each operation; the bound client cancels I/O.
func newPlayerQueue(session context.Context, client playerQueueClient) (*playerQueue, error) {
	if session == nil {
		return nil, errors.New("queue session context is nil")
	}
	if client == nil || (reflect.ValueOf(client).Kind() == reflect.Pointer && reflect.ValueOf(client).IsNil()) {
		return nil, errors.New("queue client is nil")
	}
	return &playerQueue{session: session, client: client, inFlight: make(chan struct{}, 1)}, nil
}

type playerQueueSnapshot struct {
	owner   *playerQueue
	rows    []kefw2.ContentItem
	version int
}

// items returns a copy so presentation state cannot alter the comparison token.
// Its order and zero-based indices are the speaker's queue order.
func (s *playerQueueSnapshot) items() []kefw2.ContentItem {
	if s == nil {
		return nil
	}
	return clonePlayerQueueItems(s.rows)
}

type playerQueueMode struct {
	Shuffle bool
	Repeat  string // "off", "one", or "all"
}

// Sent means the client mutation method was invoked, not that the speaker
// necessarily received it. A non-nil Snapshot or Mode is an observed readback.
// Noop means no mutation was sent and no readback is implied.
type playerQueueResult struct {
	Snapshot *playerQueueSnapshot
	Mode     *playerQueueMode
	Sent     bool
	Noop     bool
}

func (q *playerQueue) contextErr(ctx context.Context) error {
	if ctx == nil {
		return errors.New("queue operation context is nil")
	}
	if err := q.session.Err(); err != nil {
		return err
	}
	return ctx.Err()
}

func (q *playerQueue) acquire(ctx context.Context) error {
	if err := q.contextErr(ctx); err != nil {
		return err
	}
	select {
	case q.inFlight <- struct{}{}:
	default:
		return errPlayerQueueBusy
	}
	if err := q.contextErr(ctx); err != nil {
		q.release()
		return err
	}
	return nil
}

func (q *playerQueue) release() {
	<-q.inFlight
}

func (q *playerQueue) readRows(ctx context.Context) (*playerQueueSnapshot, error) {
	if err := q.contextErr(ctx); err != nil {
		return nil, err
	}
	response, err := q.client.GetPlayQueue()
	if err != nil {
		return nil, fmt.Errorf("reading the speaker queue: %w", errors.Join(err, q.contextErr(ctx)))
	}
	if err := q.contextErr(ctx); err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("reading the speaker queue: empty response")
	}
	if response.RowsRedirect != "" {
		return nil, fmt.Errorf("reading the speaker queue: unexpected redirect %q", response.RowsRedirect)
	}
	if response.RowsCount != len(response.Rows) {
		return nil, fmt.Errorf("reading the speaker queue: incomplete response (%d of %d rows)",
			len(response.Rows), response.RowsCount)
	}
	return &playerQueueSnapshot{
		owner: q, rows: clonePlayerQueueItems(response.Rows), version: response.RowsVersion,
	}, nil
}

func (q *playerQueue) readMode(ctx context.Context) (playerQueueMode, error) {
	if err := q.contextErr(ctx); err != nil {
		return playerQueueMode{}, err
	}
	mode, err := q.client.GetPlayMode()
	if err != nil {
		return playerQueueMode{}, fmt.Errorf("reading queue mode: %w", errors.Join(err, q.contextErr(ctx)))
	}
	if err := q.contextErr(ctx); err != nil {
		return playerQueueMode{}, err
	}
	switch mode {
	case kefw2.PlayModeNormal:
		return playerQueueMode{Repeat: PlayModeOff}, nil
	case kefw2.PlayModeRepeatOne:
		return playerQueueMode{Repeat: queueRepeatOne}, nil
	case kefw2.PlayModeRepeatAll:
		return playerQueueMode{Repeat: queueRepeatAll}, nil
	case kefw2.PlayModeShuffle:
		return playerQueueMode{Shuffle: true, Repeat: PlayModeOff}, nil
	case kefw2.PlayModeShuffleRepeatOne:
		return playerQueueMode{Shuffle: true, Repeat: queueRepeatOne}, nil
	case kefw2.PlayModeShuffleRepeatAll:
		return playerQueueMode{Shuffle: true, Repeat: queueRepeatAll}, nil
	default:
		return playerQueueMode{}, fmt.Errorf("speaker returned unsupported queue mode %q", mode)
	}
}

func (m playerQueueMode) wireMode() (string, error) {
	switch m.Repeat {
	case PlayModeOff:
		if m.Shuffle {
			return kefw2.PlayModeShuffle, nil
		}
		return kefw2.PlayModeNormal, nil
	case queueRepeatOne:
		if m.Shuffle {
			return kefw2.PlayModeShuffleRepeatOne, nil
		}
		return kefw2.PlayModeRepeatOne, nil
	case queueRepeatAll:
		if m.Shuffle {
			return kefw2.PlayModeShuffleRepeatAll, nil
		}
		return kefw2.PlayModeRepeatAll, nil
	default:
		return "", fmt.Errorf("invalid repeat mode %q (use off, one, or all)", m.Repeat)
	}
}

// Read and ReadMode remain available while an uncertain action is awaiting
// review. Their returned observations do not themselves acknowledge that action.
func (q *playerQueue) Read(ctx context.Context) (*playerQueueSnapshot, error) {
	if err := q.acquire(ctx); err != nil {
		return nil, err
	}
	defer q.release()
	snapshot, err := q.readRows(ctx)
	if err == nil && q.reviewKind == playerQueueReviewRows {
		q.reviewReady = true
	}
	return snapshot, err
}

func (q *playerQueue) ReadMode(ctx context.Context) (playerQueueMode, error) {
	if err := q.acquire(ctx); err != nil {
		return playerQueueMode{}, err
	}
	defer q.release()
	mode, err := q.readMode(ctx)
	if err == nil && q.reviewKind == playerQueueReviewMode {
		q.reviewReady = true
	}
	return mode, err
}

// Acknowledge requires a successful readback of the affected state after an
// uncertain action. The caller must present it and obtain explicit user review;
// this does not assert whether the original request took effect.
func (q *playerQueue) Acknowledge(ctx context.Context) error {
	if err := q.acquire(ctx); err != nil {
		return err
	}
	defer q.release()
	if q.reviewKind != playerQueueReviewed && !q.reviewReady {
		return errPlayerQueueReview
	}
	q.reviewKind = playerQueueReviewed
	q.reviewReady = false
	return nil
}

func (q *playerQueue) uncertain(
	kind playerQueueReviewKind, action string, readbackAvailable bool, causes ...error,
) error {
	q.reviewKind = kind
	q.reviewReady = readbackAvailable
	return fmt.Errorf("%w: %s: %w", errPlayerQueueUncertain, action, errors.Join(causes...))
}

func samePlayerQueueSnapshot(a, b *playerQueueSnapshot) bool {
	return a.owner == b.owner && a.version == b.version && reflect.DeepEqual(a.rows, b.rows)
}

func clonePlayerQueueItems(rows []kefw2.ContentItem) []kefw2.ContentItem {
	result := make([]kefw2.ContentItem, len(rows))
	for i, row := range rows {
		result[i] = row
		if row.MediaData != nil {
			data := *row.MediaData
			data.Resources = slices.Clone(row.MediaData.Resources)
			result[i].MediaData = &data
		}
		if row.Images != nil {
			images := *row.Images
			images.Images = slices.Clone(row.Images.Images)
			result[i].Images = &images
		}
		if row.Context != nil {
			itemContext := *row.Context
			result[i].Context = &itemContext
		}
		if row.Value != nil {
			value := *row.Value
			result[i].Value = &value
		}
	}
	return result
}

func playerQueueIndex(index, length int) error {
	if index < 0 || index >= length {
		return fmt.Errorf("queue index %d is out of range for %d rows", index, length)
	}
	return nil
}

// playerQueueDropTarget converts a drag insertion gap (0 through length) to
// the final zero-based row index accepted by Move, accounting for removal.
func playerQueueDropTarget(from, gap, length int) (int, error) {
	if err := playerQueueIndex(from, length); err != nil {
		return 0, err
	}
	if gap < 0 || gap > length {
		return 0, fmt.Errorf("queue insertion gap %d is out of range for %d rows", gap, length)
	}
	if gap > from {
		gap--
	}
	return gap, nil
}

type playerQueuePrepare func([]kefw2.ContentItem) (expected []kefw2.ContentItem, noop bool, err error)

func (q *playerQueue) changeRows(
	ctx context.Context,
	displayed *playerQueueSnapshot,
	action string,
	prepare playerQueuePrepare,
	send func([]kefw2.ContentItem) error,
) (playerQueueResult, error) {
	var result playerQueueResult
	if err := q.acquire(ctx); err != nil {
		return result, err
	}
	defer q.release()
	if q.reviewKind != playerQueueReviewed {
		return result, errPlayerQueueReview
	}
	if displayed == nil || displayed.owner != q {
		return result, errPlayerQueueWrongSession
	}
	expected, noop, err := prepare(displayed.rows)
	if err != nil {
		return result, err
	}
	if noop {
		result.Noop = true
		return result, nil
	}

	fresh, err := q.readRows(ctx)
	if err != nil {
		return result, err
	}
	if !samePlayerQueueSnapshot(displayed, fresh) {
		result.Snapshot = fresh
		return result, errPlayerQueueStale
	}
	if err := q.contextErr(ctx); err != nil {
		return result, err
	}

	result.Sent = true
	writeErr := send(fresh.rows)
	if err := q.contextErr(ctx); err != nil {
		return result, q.uncertain(playerQueueReviewRows, action, false, writeErr, err)
	}
	after, readErr := q.readRows(ctx)
	result.Snapshot = after
	if writeErr != nil || readErr != nil {
		return result, q.uncertain(playerQueueReviewRows, action, after != nil, writeErr, readErr)
	}
	if !reflect.DeepEqual(expected, after.rows) {
		return result, q.uncertain(playerQueueReviewRows, action, true,
			errors.New("readback did not match the requested queue"))
	}
	return result, nil
}

// Play sends the selected occurrence's index. Its successful result means the
// request was accepted and the queue was read back; playback needs a status read.
func (q *playerQueue) Play(
	ctx context.Context, displayed *playerQueueSnapshot, index int,
) (playerQueueResult, error) {
	return q.changeRows(ctx, displayed, "play queue item", func(rows []kefw2.ContentItem) ([]kefw2.ContentItem, bool, error) {
		if err := playerQueueIndex(index, len(rows)); err != nil {
			return nil, false, err
		}
		return clonePlayerQueueItems(rows), false, nil
	}, func(rows []kefw2.ContentItem) error {
		track := clonePlayerQueueItems(rows[index : index+1])[0]
		return q.client.PlayQueueIndex(index, &track)
	})
}

// Move's destination is the row's final zero-based index, not a drag gap.
// A move never starts playback.
func (q *playerQueue) Move(
	ctx context.Context, displayed *playerQueueSnapshot, from, to int,
) (playerQueueResult, error) {
	return q.changeRows(ctx, displayed, "move queue item", func(rows []kefw2.ContentItem) ([]kefw2.ContentItem, bool, error) {
		if err := playerQueueIndex(from, len(rows)); err != nil {
			return nil, false, err
		}
		if err := playerQueueIndex(to, len(rows)); err != nil {
			return nil, false, err
		}
		if from == to {
			return nil, true, nil
		}
		expected := clonePlayerQueueItems(rows)
		item := expected[from]
		if from < to {
			copy(expected[from:to], expected[from+1:to+1])
		} else {
			copy(expected[to+1:from+1], expected[to:from])
		}
		expected[to] = item
		return expected, false, nil
	}, func(_ []kefw2.ContentItem) error {
		return q.client.MoveQueueItem(from, to)
	})
}

// Remove is the explicit action on one selected row; it does not require the
// separate confirmation required by Clear.
func (q *playerQueue) Remove(
	ctx context.Context, displayed *playerQueueSnapshot, index int,
) (playerQueueResult, error) {
	return q.changeRows(ctx, displayed, "remove queue item", func(rows []kefw2.ContentItem) ([]kefw2.ContentItem, bool, error) {
		if err := playerQueueIndex(index, len(rows)); err != nil {
			return nil, false, err
		}
		expected := clonePlayerQueueItems(rows)
		expected = append(expected[:index], expected[index+1:]...)
		return expected, false, nil
	}, func(_ []kefw2.ContentItem) error {
		return q.client.RemoveFromQueue([]int{index})
	})
}

// Clear requires explicit confirmation and compares the snapshot displayed
// before that confirmation, so newly added rows are not silently removed.
func (q *playerQueue) Clear(
	ctx context.Context, displayed *playerQueueSnapshot, confirmed bool,
) (playerQueueResult, error) {
	if err := q.contextErr(ctx); err != nil {
		return playerQueueResult{}, err
	}
	if !confirmed {
		return playerQueueResult{}, errPlayerQueueConfirmation
	}
	return q.changeRows(ctx, displayed, "clear queue", func(rows []kefw2.ContentItem) ([]kefw2.ContentItem, bool, error) {
		if len(rows) == 0 {
			return nil, true, nil
		}
		return []kefw2.ContentItem{}, false, nil
	}, func(_ []kefw2.ContentItem) error {
		return q.client.ClearPlaylist()
	})
}

func (q *playerQueue) changeMode(
	ctx context.Context, action string, change func(playerQueueMode) playerQueueMode,
) (playerQueueResult, error) {
	var result playerQueueResult
	if err := q.acquire(ctx); err != nil {
		return result, err
	}
	defer q.release()
	if q.reviewKind != playerQueueReviewed {
		return result, errPlayerQueueReview
	}
	before, err := q.readMode(ctx)
	if err != nil {
		return result, err
	}
	desired := change(before)
	wireMode, err := desired.wireMode()
	if err != nil {
		return result, err
	}
	if desired == before {
		result.Mode = &before
		result.Noop = true
		return result, nil
	}
	if err := q.contextErr(ctx); err != nil {
		return result, err
	}

	result.Sent = true
	writeErr := q.client.SetPlayMode(wireMode)
	if err := q.contextErr(ctx); err != nil {
		return result, q.uncertain(playerQueueReviewMode, action, false, writeErr, err)
	}
	after, readErr := q.readMode(ctx)
	if readErr == nil {
		result.Mode = &after
	}
	if writeErr != nil || readErr != nil {
		return result, q.uncertain(playerQueueReviewMode, action, readErr == nil, writeErr, readErr)
	}
	if after != desired {
		return result, q.uncertain(playerQueueReviewMode, action, true,
			errors.New("readback did not match the requested queue mode"))
	}
	return result, nil
}

func (q *playerQueue) SetShuffle(ctx context.Context, enabled bool) (playerQueueResult, error) {
	return q.changeMode(ctx, "set shuffle", func(mode playerQueueMode) playerQueueMode {
		mode.Shuffle = enabled
		return mode
	})
}

func (q *playerQueue) SetRepeat(ctx context.Context, repeat string) (playerQueueResult, error) {
	if err := q.contextErr(ctx); err != nil {
		return playerQueueResult{}, err
	}
	if _, err := (playerQueueMode{Repeat: repeat}).wireMode(); err != nil {
		return playerQueueResult{}, err
	}
	return q.changeMode(ctx, "set repeat", func(mode playerQueueMode) playerQueueMode {
		mode.Repeat = repeat
		return mode
	})
}
