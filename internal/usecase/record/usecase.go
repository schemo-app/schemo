// Package record owns the history: what was sent, what came back, and how long any of it is
// kept.
package record

import (
	"sync/atomic"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Event topics. A record appearing and a send finishing are different things: the first is one the
// app was told about, the second is an answer to something the window asked for. There is no
// "started" topic: the caller gets the id from Send itself, and it is the only caller there is.
const (
	TopicRecordAdded     = "record:added"
	TopicRequestFinished = "request:finished"
	TopicRequestFailed   = "request:failed"
	// A whole history going, which happens in the settings window and is drawn in the main one: a
	// list that was told about every record one by one has to be told about this too, or it goes on
	// drawing rows that are no longer there.
	TopicHistoryCleared = "history:cleared"
)

type UseCase struct {
	store     Store
	scope     domain.Scope
	jar       Jar
	executor  Executor
	notifier  domain.Notifier
	retention RetentionSource
	source    Source
	screen    Screener
	mask      Masker
	ids       platform.IDGen
	build     platform.BuildInfo

	// sincePrune counts saves between two looks at the retention rules. It is atomic because a
	// window's send and a collection run save from goroutines of their own, and two sends landing on
	// the same counter would be a data race on a number that decides when history is trimmed.
	sincePrune atomic.Int32
}

func NewUseCase(
	store Store,
	scope domain.Scope,
	jar Jar,
	executor Executor,
	notifier domain.Notifier,
	retention RetentionSource,
	source Source,
	screen Screener,
	mask Masker,
	ids platform.IDGen,
	build platform.BuildInfo,
) *UseCase {
	return &UseCase{
		store: store, scope: scope, jar: jar, executor: executor, notifier: notifier,
		retention: retention, source: source, screen: screen, mask: mask, ids: ids, build: build,
	}
}
