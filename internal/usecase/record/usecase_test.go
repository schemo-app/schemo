package record

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// fakeScope answers which workspace the window is showing. The store below keeps one history and
// ignores the id, but records every one it was called with: the id the scope answers with has to be
// the id the store is asked in, and that is what askedIn is for.
type fakeScope struct{ id string }

func (f fakeScope) ActiveWorkspace(context.Context) (string, error) {
	if f.id == "" {
		return domain.WorkspacePersonalID, nil
	}
	return f.id, nil
}

type fakeStore struct {
	saved    []domain.Record
	bodies   map[string]string
	pruned   []domain.PruneOptions
	listed   []int
	saveFail error
	askedIn  []string
	cached   []domain.Record
	queued   []string
	fetched  []domain.RecordFetched
}

func newFakeStore() *fakeStore {
	return &fakeStore{bodies: map[string]string{}}
}

func (f *fakeStore) SaveRecord(_ context.Context, workspaceID string, rec domain.Record) error {
	f.askedIn = append(f.askedIn, workspaceID)
	if f.saveFail != nil {
		return f.saveFail
	}
	for _, existing := range f.saved {
		if existing.ID == rec.ID {
			return domain.ErrConflict
		}
	}
	f.saved = append(f.saved, rec)
	if rec.ResponseBody != nil {
		f.bodies[rec.ID] = rec.ResponseBody.Inline
	}
	return nil
}

// QueueRecord is the offer of a record to the server. The fake keeps what it was asked about, which
// is what the tests that a record is offered once read.
func (f *fakeStore) QueueRecord(_ context.Context, _, id string) error {
	f.queued = append(f.queued, id)
	return nil
}

func (f *fakeStore) Record(_ context.Context, id string) (domain.Record, error) {
	for _, rec := range f.saved {
		if rec.ID == id {
			return rec, nil
		}
	}
	return domain.Record{}, domain.ErrNotFound
}

func (f *fakeStore) Records(
	_ context.Context,
	workspaceID string,
	source domain.RecordSource,
	limit int,
) ([]domain.Record, error) {
	f.askedIn = append(f.askedIn, workspaceID)
	f.listed = append(f.listed, limit)
	out := []domain.Record{}
	for _, rec := range f.saved {
		if source != "" && rec.Source != source {
			continue
		}
		out = append(out, rec)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// RecordsBefore is the newest records that started before a moment, like the real list.
func (f *fakeStore) RecordsBefore(
	_ context.Context,
	_ string,
	before int64,
	limit int,
) ([]domain.Record, error) {
	var out []domain.Record
	for _, rec := range f.saved {
		if rec.StartedAt < before {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt > out[b].StartedAt })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) SaveCached(_ context.Context, _ string, records []domain.Record) error {
	f.cached = append(f.cached, records...)
	f.saved = append(f.saved, records...)
	return nil
}

func (f *fakeStore) ReadBody(_ context.Context, id string, _ domain.BodySide) (string, error) {
	body, ok := f.bodies[id]
	if !ok {
		return "", domain.ErrNotFound
	}
	return body, nil
}

// SaveFetched keeps what a fetch answered, and overwrites nothing, like the real store. It keeps
// the whole answer as handed over: what a fetch brings beside the body is the store's business, and
// the test that reads it is about the use case not dropping any of it on the way.
func (f *fakeStore) SaveFetched(
	_ context.Context,
	_ string,
	id string,
	fetched domain.RecordFetched,
) error {
	if f.bodies == nil {
		f.bodies = map[string]string{}
	}
	for _, body := range fetched.Bodies {
		if _, taken := f.bodies[id]; !taken {
			f.bodies[id] = body.Inline
		}
	}
	f.fetched = append(f.fetched, fetched)
	return nil
}

func (f *fakeStore) SaveCookieWrites(
	_ context.Context,
	_, id string,
	writes []domain.CookieWrite,
) error {
	for i, rec := range f.saved {
		if rec.ID == id {
			f.saved[i].CookieWrites = writes
		}
	}
	return nil
}

func (f *fakeStore) DeleteRecords(_ context.Context, _ string, ids []string) error {
	doomed := map[string]bool{}
	for _, id := range ids {
		doomed[id] = true
	}
	kept := f.saved[:0]
	for _, rec := range f.saved {
		if !doomed[rec.ID] {
			kept = append(kept, rec)
		}
	}
	f.saved = kept
	return nil
}

func (f *fakeStore) Prune(
	_ context.Context,
	workspaceID string,
	opts domain.PruneOptions,
) (int, error) {
	f.askedIn = append(f.askedIn, workspaceID)
	f.pruned = append(f.pruned, opts)
	return 0, nil
}

// DeleteAllRecords empties one space: the records made in it, and not the ones beside them.
func (f *fakeStore) DeleteAllRecords(_ context.Context, workspaceID string) (int, error) {
	f.askedIn = append(f.askedIn, workspaceID)
	kept := f.saved[:0]
	gone := 0
	for _, rec := range f.saved {
		if rec.WorkspaceID == workspaceID {
			gone++
			continue
		}
		kept = append(kept, rec)
	}
	f.saved = kept
	return gone, nil
}

// HistoryStats weighs one space's records and the bodies kept for them.
func (f *fakeStore) HistoryStats(
	_ context.Context,
	workspaceID string,
) (domain.HistoryStats, error) {
	f.askedIn = append(f.askedIn, workspaceID)
	var stats domain.HistoryStats
	for _, rec := range f.saved {
		if rec.WorkspaceID != workspaceID {
			continue
		}
		stats.Count++
		if rec.ResponseBody != nil {
			stats.Bytes += rec.ResponseBody.Size
		}
	}
	return stats, nil
}

type fakeExecutor struct {
	got       []Request
	response  domain.Response
	cancelled []string
	stopEarly bool
}

func (f *fakeExecutor) Execute(_ context.Context, req Request) (*domain.Response, error) {
	f.got = append(f.got, req)
	resp := f.response
	if f.stopEarly {
		resp = domain.Response{Cancelled: true}
	}
	return &resp, nil
}

func (f *fakeExecutor) Cancel(id string) bool {
	f.cancelled = append(f.cancelled, id)
	return true
}

type fakeNotifier struct {
	events chan struct {
		topic   string
		payload any
	}
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{events: make(chan struct {
		topic   string
		payload any
	}, 16)}
}

func (f *fakeNotifier) Publish(topic string, payload any) {
	f.events <- struct {
		topic   string
		payload any
	}{topic, payload}
}

// waitFor reads until the topic arrives, so an asynchronous send can be checked without sleeping.
func (f *fakeNotifier) waitFor(t *testing.T, topic string) any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-f.events:
			if event.topic == topic {
				return event.payload
			}
		case <-deadline:
			t.Fatalf("no %q event arrived", topic)
		}
	}
}

// fakeScreen is the scripting feature seen from here: it keeps the passes it was handed and can
// change the request or call it off, which is everything this feature has to act on.
type fakeScreen struct {
	passes []domain.ScriptPass
	skip   bool
	fail   error
	change func(pass *domain.ScriptPass)

	// What the store held when each pass ran. A report hangs off the record it ran around, so the
	// moment the post-response scripts are handed the attempt is a moment the record has to exist in.
	records       *fakeStore
	savedAtBefore int
	savedAtAfter  int
}

func (f *fakeScreen) Before(_ context.Context, _ string, pass *domain.ScriptPass) (bool, error) {
	if f.records != nil {
		f.savedAtBefore = len(f.records.saved)
	}
	if f.fail != nil {
		return false, f.fail
	}
	if f.change != nil {
		f.change(pass)
	}
	f.passes = append(f.passes, *pass)
	return f.skip, nil
}

func (f *fakeScreen) After(_ context.Context, _ string, pass domain.ScriptPass) {
	if f.records != nil {
		f.savedAtAfter = len(f.records.saved)
	}
	f.passes = append(f.passes, pass)
}

// fakeMask is the mask asked for again, for a request a script made into something else.
type fakeMask struct {
	asked  int
	masked Masked
	fail   error
	// seen is the attempt the mask was asked about, so a test can say what it was given to render.
	seen SendInput

	// secrets are the values this fake holds, replaced wherever they sit in a text, and texts is
	// what the last MaskTexts was asked about.
	secrets []string
	failAt  error
	texts   []string
}

func (f *fakeMask) Mask(_ context.Context, in SendInput, _ domain.ScriptRequest) (Masked, error) {
	f.asked++
	f.seen = in
	if f.fail != nil {
		return Masked{}, f.fail
	}
	return f.masked, nil
}

// MaskTexts leaves each known value as its mask, which is what the real one does with the values of
// the space.
func (f *fakeMask) MaskTexts(_ context.Context, _ string, texts []string) ([]string, error) {
	f.texts = append(f.texts, texts...)
	if f.failAt != nil {
		return nil, f.failAt
	}
	out := make([]string, len(texts))
	for i, text := range texts {
		for _, secret := range f.secrets {
			text = strings.ReplaceAll(text, secret, "***")
		}
		out[i] = text
	}
	return out, nil
}

func newUseCase() (*UseCase, *fakeStore, *fakeExecutor, *fakeNotifier) {
	uc, store, executor, notifier, _ := newScriptedUseCase(nil, nil)
	return uc, store, executor, notifier
}

func newScriptedUseCase(
	screen Screener,
	mask Masker,
) (*UseCase, *fakeStore, *fakeExecutor, *fakeNotifier, *fakeJar) {
	return newScopedUseCase(fakeScope{}, screen, mask)
}

// newScopedUseCase is newScriptedUseCase with the workspace the scope answers with named by the
// caller, which is what the test that checks the id travels needs.
func newScopedUseCase(
	scope domain.Scope,
	screen Screener,
	mask Masker,
) (*UseCase, *fakeStore, *fakeExecutor, *fakeNotifier, *fakeJar) {
	store := newFakeStore()
	micros := func(us int64) *int64 { return &us }
	executor := &fakeExecutor{response: domain.Response{
		Status: 200, StatusText: "200 OK", Body: `{"data":[]}`, DurationUs: 12000,
		WaitUs: micros(9000), DownloadUs: micros(3000),
	}}
	notifier := newFakeNotifier()
	jar := &fakeJar{}
	retention := RetentionSourceFunc(func(context.Context) (domain.Retention, error) {
		return domain.RetainWeek, nil
	})
	return NewUseCase(store, scope, jar, executor, notifier, retention, &fakeSource{}, screen, mask,
			platform.NewIDGen(), platform.BuildInfo{Name: "Schemo", Version: "test"}), store,
		executor, notifier, jar
}

// fakeSource records what it was asked, to show a body is fetched once and then kept.
type fakeSource struct {
	fetched domain.RecordFetched
	err     error
	asked   []string
	// older is the page the server holds past the window, and olderFrom what it was asked from.
	older     []domain.Record
	olderFrom []int64
}

func (f *fakeSource) Older(
	_ context.Context,
	_ string,
	before int64,
	_ int,
) ([]domain.Record, error) {
	f.olderFrom = append(f.olderFrom, before)
	return f.older, f.err
}

func (f *fakeSource) Fetched(_ context.Context, _, id string) (domain.RecordFetched, error) {
	f.asked = append(f.asked, id)
	return f.fetched, f.err
}

// fakeJar is the jar seen from a record, which is the half of it this feature still owns: taking
// a write back. Putting one in is the engine's — it is the side that knows the redirects, and a
// record keeps the list it was answered with rather than asking the jar itself.
type fakeJar struct {
	undone []domain.CookieWrite
	fail   error
}

func (f *fakeJar) Undo(_ context.Context, _ string, writes []domain.CookieWrite) error {
	f.undone = writes
	return f.fail
}

func input() SendInput {
	return SendInput{
		Method:        "GET",
		URL:           "https://api.example.com/articles?token=s3cret",
		Headers:       []domain.HeaderPair{{Name: "Authorization", Value: "Bearer s3cret"}},
		Body:          "",
		MaskedURL:     "https://api.example.com/articles?token=••••",
		MaskedHeaders: []domain.HeaderPair{{Name: "Authorization", Value: "Bearer ••••"}},
	}
}

// The credential the engine computed is on no list above it — a Digest answer comes from a
// challenge that had not arrived when the request was prepared — so the record learns it from what
// came back. A record without it would be a record of the request that was refused.
func TestSendRecordsWhatTheEngineAdded(t *testing.T) {
	uc, _, executor, notifier := newUseCase()
	executor.response.SentHeaders = []domain.HeaderPair{
		{Name: "Authorization", Value: "Digest " + domain.SecretMask},
	}

	in := input()
	in.Headers = nil
	in.MaskedHeaders = nil
	in.Digest = &domain.DigestCredentials{Username: "user", Password: "pass"}
	if _, err := uc.Send(context.Background(), in); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// The answer is what says the attempt is over; the engine has been asked by then.
	finished := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)

	// The credential itself goes to the engine rather than onto the request: there is nothing to put
	// on it until the server has said how.
	if len(executor.got) != 1 || executor.got[0].Digest == nil ||
		executor.got[0].Digest.Username != "user" {
		t.Errorf("sent = %+v, want the credential handed to the engine", executor.got)
	}

	// The row is folded in as the engine handed it over: masking a computed credential is the
	// engine's business, and the record deciding it a second time is how the two copies drift.
	if len(finished.Record.RequestHeaders) != 1 ||
		finished.Record.RequestHeaders[0] != executor.response.SentHeaders[0] {
		t.Errorf("recorded headers = %+v, want the engine's own row folded in",
			finished.Record.RequestHeaders)
	}
}

// What the exchange did to the jar is the record's, and it comes from the engine: the engine is the
// side that asks the jar, once per hop, and the window draws the list it answers with. What this
// feature has to get right is that the list reaches the record, and that the send goes out in the
// space the record will be filed in — a jar belongs to a workspace.
func TestSendRecordsWhatTheAnswerDidToTheJar(t *testing.T) {
	uc, _, executor, notifier, _ := newScriptedUseCase(nil, nil)
	executor.response.CookieWrites = []domain.CookieWrite{
		{Name: "session", Value: "abc", State: domain.CookieAdded},
	}

	if _, err := uc.Send(context.Background(), input()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	finished := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)

	if executor.got[0].Workspace != domain.WorkspacePersonalID {
		t.Errorf("sent in %q, want the space the record is filed in", executor.got[0].Workspace)
	}
	writes := finished.Record.CookieWrites
	if len(writes) != 1 || writes[0].Name != "session" {
		t.Fatalf("recorded cookie writes = %+v, want what the engine was answered", writes)
	}
}

// Taking a write back is the record's id going to the jar: the record holds what the answer
// changed, and the window asks by id rather than holding the list itself.
func TestUndoAsksTheJarWithWhatTheRecordKept(t *testing.T) {
	uc, store, executor, notifier, jar := newScriptedUseCase(nil, nil)
	executor.response.CookieWrites = []domain.CookieWrite{
		{Name: "session", Value: "abc", State: domain.CookieAdded},
	}

	if _, err := uc.Send(context.Background(), input()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	finished := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)
	id := finished.Record.ID

	if err := uc.UndoCookieWrites(context.Background(), id); err != nil {
		t.Fatalf("UndoCookieWrites: %v", err)
	}
	if len(jar.undone) != 1 || jar.undone[0].Name != "session" {
		t.Errorf("undone = %+v, want what the record kept", jar.undone)
	}
	if len(store.saved) != 1 {
		t.Errorf("saved %d records, want the one the send made", len(store.saved))
	}
	// The record stops claiming a write that has been taken back: the window reads the diff off the
	// record, and a record still claiming it would offer to undo it a second time.
	if kept := store.saved[0].CookieWrites; len(kept) != 0 {
		t.Errorf("the record still holds %+v, want the write taken back", kept)
	}
}

// A cookie the jar never took is not a write to take back, and undoing the rest must not wipe it:
// what the answer said is a fact about that answer, and a blocked domain or a record this app did
// not send has no change to reverse.
func TestUndoKeepsWhatWasNeverSaved(t *testing.T) {
	uc, store, executor, notifier, _ := newScriptedUseCase(nil, nil)
	executor.response.CookieWrites = []domain.CookieWrite{
		{Name: "session", Value: "abc", State: domain.CookieAdded},
		{Name: "blocked", Value: "x", State: domain.CookieNotSaved},
	}

	if _, err := uc.Send(context.Background(), input()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	finished := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)

	if err := uc.UndoCookieWrites(context.Background(), finished.Record.ID); err != nil {
		t.Fatalf("UndoCookieWrites: %v", err)
	}
	kept := store.saved[0].CookieWrites
	if len(kept) != 1 || kept[0].Name != "blocked" {
		t.Errorf("the record holds %+v, want only the cookie the jar refused", kept)
	}
}

// A record remembers the request of a collection it came from, which is what makes the history of a
// request readable as its own sends — the ones a run made and the ones a person made, one list.
func TestASendRemembersTheRequestItCameFrom(t *testing.T) {
	uc, _, _, notifier := newUseCase()

	in := input()
	in.Node = "r-1"
	if _, err := uc.Send(context.Background(), in); err != nil {
		t.Fatalf("Send: %v", err)
	}
	finished := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)
	if finished.Record.NodeID != "r-1" {
		t.Errorf("node = %q, want the request this was sent from", finished.Record.NodeID)
	}

	// The command line composes a request rather than keeping one, and a record of it names no node:
	// a draft is not a request of any collection.
	line := input()
	line.Node = string(domain.DraftCommandLine)
	if _, err := uc.Send(context.Background(), line); err != nil {
		t.Fatalf("Send: %v", err)
	}
	fromLine := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)
	if fromLine.Record.NodeID != "" {
		t.Errorf("node = %q, want nothing for a request sent from the command line",
			fromLine.Record.NodeID)
	}
}

func TestSendRecordsTheMaskedRequest(t *testing.T) {
	uc, store, executor, notifier := newUseCase()

	id, err := uc.Send(context.Background(), input())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	finished, ok := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)
	if !ok {
		t.Fatal("the finished event is not a RequestFinished")
	}
	if finished.ID != id {
		t.Errorf("finished %q, want the id Send returned", finished.ID)
	}

	// What the engine got is the real request; what history keeps is the masked one.
	if len(executor.got) != 1 {
		t.Fatalf("the engine was asked %d times, want once", len(executor.got))
	}
	if executor.got[0].URL != input().URL {
		t.Errorf("sent %q, want the unmasked URL", executor.got[0].URL)
	}

	rec := finished.Record
	if rec.URL != input().MaskedURL {
		t.Errorf("recorded url = %q, want the masked one", rec.URL)
	}
	if len(rec.RequestHeaders) != 1 || rec.RequestHeaders[0].Value != "Bearer ••••" {
		t.Errorf("recorded headers = %+v, want the masked ones", rec.RequestHeaders)
	}
	if rec.Status != 200 || rec.Source != domain.SourceManual || rec.DurationUs != 12000 {
		t.Errorf("record = %+v, want a 200 from the request builder", rec.RecordSummary)
	}
	// The phases the engine measured travel as they are, and the ones it did not are absent.
	if rec.WaitUs == nil || *rec.WaitUs != 9000 || rec.DNSUs != nil {
		t.Errorf("phases = wait %v, dns %v; want the measured wait and no dial", rec.WaitUs, rec.DNSUs)
	}
	if rec.ResponseBody == nil || rec.ResponseBody.Inline != `{"data":[]}` {
		t.Errorf("responseBody = %+v, want the text the engine returned", rec.ResponseBody)
	}
	if len(store.saved) != 1 || store.saved[0].ID != id {
		t.Errorf("saved %d records, want the one that finished", len(store.saved))
	}
}

// The scripts run around the attempt — before it goes out with a request they may change, and
// after the answer came back. What goes out is what they left.
func TestTheScriptsRunAroundTheAttempt(t *testing.T) {
	screen := &fakeScreen{change: func(pass *domain.ScriptPass) {
		pass.Request.Headers = append(pass.Request.Headers,
			domain.HeaderPair{Name: "X-Sign", Value: "подпись"})
	}}
	mask := &fakeMask{masked: Masked{
		URL:     "https://api.example.com/articles?token=••••&sign=••••",
		Headers: []domain.HeaderPair{{Name: "X-Sign", Value: "••••"}},
	}}
	uc, _, executor, notifier, _ := newScriptedUseCase(screen, mask)

	in := input()
	in.Node, in.Run = "r-1", "run-1"
	id, err := uc.Send(context.Background(), in)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	finished, _ := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)

	if len(screen.passes) != 2 {
		t.Fatalf("the scripts were asked %d times, want once before and once after", len(screen.passes))
	}
	before, after := screen.passes[0], screen.passes[1]
	if before.NodeID != "r-1" || before.Run != "run-1" || before.RecordID != id {
		t.Errorf("pass before = %+v, want it named by node, run and record", before)
	}
	if before.Response != nil {
		t.Error("a pre-request script was handed an answer that does not exist yet")
	}
	if after.Response == nil || after.Response.Status != 200 {
		t.Errorf("pass after = %+v, want the answer", after)
	}

	// What was sent is what the pre-request script left, and what is written down is that request
	// masked — the preparation could not know what a script would add.
	if len(executor.got[0].Headers) != 2 || executor.got[0].Headers[1].Name != "X-Sign" {
		t.Errorf("sent headers = %+v, want the one the script added", executor.got[0].Headers)
	}
	if mask.asked != 1 {
		t.Errorf("the mask was asked %d times, want once for the request that changed", mask.asked)
	}
	if finished.Record.URL != mask.masked.URL {
		t.Errorf("recorded url = %q, want the request as it went out, masked", finished.Record.URL)
	}
	if len(finished.Record.RequestHeaders) != 1 || finished.Record.RequestHeaders[0].Name != "X-Sign" {
		t.Errorf("recorded headers = %+v, want the masked ones from the second preparation",
			finished.Record.RequestHeaders)
	}
}

// The reports of a request hang off its record, so the record has to be there when they are
// written: the pre-request scripts run before anything is stored — there is nothing to store yet —
// and the post-response ones after it. Writing a report first leaves it belonging to nothing, which
// is a tab that says a request has no scripts while its scripts are sitting in the database.
func TestTheReportsBelongToARecordThatExists(t *testing.T) {
	screen := &fakeScreen{}
	uc, store, _, notifier, _ := newScriptedUseCase(screen, nil)
	screen.records = store

	in := input()
	in.Node = "r-1"
	if _, err := uc.Send(context.Background(), in); err != nil {
		t.Fatalf("Send: %v", err)
	}
	notifier.waitFor(t, TopicRequestFinished)

	if screen.savedAtBefore != 0 {
		t.Errorf("the pre-request scripts ran with %d record(s) stored, want none", screen.savedAtBefore)
	}
	if screen.savedAtAfter != 1 {
		t.Errorf("the post-response scripts ran with %d record(s) stored, want the one they belong to",
			screen.savedAtAfter)
	}
}

// A record is offered once the reports of the code around it are written, and not as it is saved: a
// push that went out between the two would send the record without them.
func TestARecordIsOfferedOnceItsScriptsHaveReported(t *testing.T) {
	uc, store, _, notifier, _ := newScriptedUseCase(&fakeScreen{}, nil)

	in := input()
	in.Node = "r-1"
	id, err := uc.Send(context.Background(), in)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	notifier.waitFor(t, TopicRequestFinished)

	if len(store.queued) != 1 || store.queued[0] != id {
		t.Errorf("offered = %v, want the record under its own id", store.queued)
	}
}

// The other side of it: a request sent with no code around it has nothing to wait for, and is
// offered the same once.
func TestARequestWithNoScriptsIsOfferedOnce(t *testing.T) {
	uc, store, _, notifier, _ := newScriptedUseCase(nil, nil)

	in := input()
	in.Node = "r-1"
	if _, err := uc.Send(context.Background(), in); err != nil {
		t.Fatalf("Send: %v", err)
	}
	notifier.waitFor(t, TopicRequestFinished)

	if len(store.queued) != 1 {
		t.Errorf("offered = %v, want the record offered once", store.queued)
	}
}

// A capture from the browser has no code around it either, and goes to the server's queue as it is
// kept.
func TestABrowserCaptureIsOfferedOnce(t *testing.T) {
	uc, store, _, _, _ := newScriptedUseCase(nil, nil)

	rec, err := uc.Ingest(context.Background(),
		IngestInput{Method: "GET", URL: "https://api.example.com"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if len(store.queued) != 1 || store.queued[0] != rec.ID {
		t.Errorf("offered = %v, want the captured record under its own id", store.queued)
	}
}

// A request the scripts did not touch is already masked by the caller's preparation, and asking for
// the same answer twice would be two substitutions per request of every run.
func TestAnUnchangedRequestIsNotMaskedAgain(t *testing.T) {
	uc, _, _, notifier, _ := newScriptedUseCase(&fakeScreen{}, &fakeMask{})

	in := input()
	in.Node = "r-1"
	if _, err := uc.Send(context.Background(), in); err != nil {
		t.Fatalf("Send: %v", err)
	}
	finished, _ := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)

	if finished.Record.URL != in.MaskedURL {
		t.Errorf("recorded url = %q, want the preparation's mask", finished.Record.URL)
	}
}

func TestASkippedRequestNeverGoesOut(t *testing.T) {
	uc, store, executor, notifier, _ := newScriptedUseCase(&fakeScreen{skip: true}, &fakeMask{})

	in := input()
	in.Node = "r-1"
	id, err := uc.Send(context.Background(), in)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	finished, _ := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)

	if len(executor.got) != 0 {
		t.Errorf("the engine was asked %d times, want nothing sent", len(executor.got))
	}
	if len(store.saved) != 0 {
		t.Errorf("history kept %d records, want nothing", len(store.saved))
	}
	if !finished.Record.Skipped || finished.Record.Status != 0 {
		t.Errorf("record = %+v, want it saying the request was not sent", finished.Record)
	}
	if finished.Record.ID != id {
		t.Errorf("record = %+v, want the id Send returned", finished.Record)
	}
}

// A script that cannot run at all is this side failing, and the attempt says so rather than sending
// a request whose scripts never happened.
func TestAScriptThatCannotRunFailsTheAttempt(t *testing.T) {
	screen := &fakeScreen{fail: errors.New("база недоступна")}
	uc, _, executor, notifier, _ := newScriptedUseCase(screen, &fakeMask{})

	in := input()
	in.Node = "r-1"
	id, err := uc.Send(context.Background(), in)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	failed, ok := notifier.waitFor(t, TopicRequestFailed).(RequestFailed)

	if !ok || failed.ID != id || !strings.Contains(failed.Error, "база недоступна") {
		t.Errorf("failure = %+v, want the reason the scripts could not run", failed)
	}
	if len(executor.got) != 0 {
		t.Error("the request went out although its scripts could not run")
	}
}

func TestSendReportsAStoreFailure(t *testing.T) {
	uc, store, _, notifier := newUseCase()
	store.saveFail = errors.New("disk is full")

	if _, err := uc.Send(context.Background(), input()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	failed, ok := notifier.waitFor(t, TopicRequestFailed).(RequestFailed)
	if !ok {
		t.Fatal("the failed event is not a RequestFailed")
	}
	if failed.Error == "" {
		t.Error("the failure carried no reason")
	}
}

func TestCancelReachesTheEngine(t *testing.T) {
	uc, _, executor, _ := newUseCase()

	if !uc.Cancel("some-id") {
		t.Error("Cancel reported nothing to stop")
	}
	if len(executor.cancelled) != 1 || executor.cancelled[0] != "some-id" {
		t.Errorf("cancelled = %v, want the id it was given", executor.cancelled)
	}
}

// An attempt the user stopped is not history: nothing came back, and the window that stopped it
// already knows it did.
func TestStoppedAttemptIsNotRecorded(t *testing.T) {
	uc, store, executor, notifier := newUseCase()
	executor.stopEarly = true

	id, err := uc.Send(context.Background(), input())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Nothing is published, so the check is that nothing arrived at all.
	select {
	case event := <-notifier.events:
		t.Fatalf("%s arrived for a stopped attempt (%s)", event.topic, id)
	case <-time.After(50 * time.Millisecond):
	}
	if len(store.saved) != 0 {
		t.Errorf("saved %d records for an attempt that was stopped", len(store.saved))
	}
}

// An ingested record is one this app did not send. Without a source it is the browser's, because
// the extension is the only thing outside the app that reports one.
func TestIngestStoresAndAnnounces(t *testing.T) {
	uc, store, _, notifier := newUseCase()

	rec, err := uc.Ingest(context.Background(), IngestInput{
		Method:         "GET",
		URL:            "https://api.example.com/articles",
		Status:         200,
		StatusText:     "200 OK",
		RequestHeaders: []domain.HeaderPair{{Name: "Accept", Value: "application/json"}},
		ResponseHeaders: []domain.HeaderPair{{Name: "Set-Cookie", Value: "a=1"},
			{Name: "Set-Cookie", Value: "b=2"}},
		ResponseBody: `{"data":[]}`,
		DurationMs:   12,
		WaitMs:       3,
		TabID:        4,
		TabTitle:     "Example",
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if rec.Source != domain.SourceBrowser || rec.TabID != 4 {
		t.Errorf("record = %+v, want a browser record with its tab", rec.RecordSummary)
	}
	if rec.StartedAt == 0 {
		t.Error("a record with no timestamp was not given one")
	}
	// What the browser timed is milliseconds; what is recorded is microseconds, and a phase it did
	// not report stays absent.
	if rec.DurationUs != 12_000 || rec.WaitUs == nil || *rec.WaitUs != 3_000 || rec.DNSUs != nil {
		t.Errorf("timings = %dus, wait %v, dns %v", rec.DurationUs, rec.WaitUs, rec.DNSUs)
	}

	announced, ok := notifier.waitFor(t, TopicRecordAdded).(domain.Record)
	if !ok {
		t.Fatal("the added event did not carry a record")
	}
	if announced.ID != rec.ID {
		t.Errorf("announced %q, stored %q", announced.ID, rec.ID)
	}
	if len(store.saved) != 1 {
		t.Errorf("saved %d records, want one", len(store.saved))
	}
}

// headerValue is the value of the one header of that name, and it fails the test when the record
// carries none: a header a mask dropped would otherwise pass every assertion about its value.
func headerValue(t *testing.T, headers []domain.HeaderPair, name string) string {
	t.Helper()
	for _, header := range headers {
		if header.Name == name {
			return header.Value
		}
	}
	t.Fatalf("the record carries no %s header", name)
	return ""
}

// A capture is a request this app did not send, and it is written down the way a send is: a value
// the device holds as a secret is left as its mask wherever the browser reported it. The answer is
// not masked, because a send leaves it as it came — what protects it is what leaves the device.
func TestACaptureIsWrittenWithTheSecretsItCarriesMasked(t *testing.T) {
	uc, _, _, _, _ := newScriptedUseCase(nil, &fakeMask{secrets: []string{"s3cret"}})

	rec, err := uc.Ingest(context.Background(), IngestInput{
		Method:      "GET",
		URL:         "https://api.example.com/articles?token=s3cret&page=2",
		TabURL:      "https://example.com/page?token=s3cret",
		RequestBody: `{"token":"s3cret"}`,
		RequestHeaders: []domain.HeaderPair{
			{Name: "Authorization", Value: "Bearer s3cret"},
			{Name: "Accept", Value: "application/json"},
		},
		ResponseHeaders: []domain.HeaderPair{{Name: "Set-Cookie", Value: "a=s3cret"}},
		ResponseBody:    `{"token":"s3cret"}`,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if rec.URL != "https://api.example.com/articles?token=***&page=2" {
		t.Errorf("url = %q, want the value in it masked", rec.URL)
	}
	if rec.TabURL != "https://example.com/page?token=***" {
		t.Errorf("tab url = %q, want the address it was captured from masked too", rec.TabURL)
	}
	if got := headerValue(t, rec.RequestHeaders, "Authorization"); got != "Bearer ***" {
		t.Errorf("authorization = %q, want the value it went out with masked", got)
	}
	if got := headerValue(t, rec.RequestHeaders, "Accept"); got != "application/json" {
		t.Errorf("accept = %q, want a header carrying no secret left alone", got)
	}
	if rec.RequestBody.Inline != `{"token":"***"}` {
		t.Errorf("request body = %q, want the value in it masked", rec.RequestBody.Inline)
	}
	if got := headerValue(t, rec.ResponseHeaders, "Set-Cookie"); got != "a=s3cret" {
		t.Errorf("set-cookie = %q, want the answer left as it came, as a send leaves it", got)
	}
}

// A mask that cannot be made leaves no record behind: a capture written down with the values the
// browser reported is a secret written out in the clear, and the caller is owed the failure.
func TestACaptureWhoseMaskCannotBeMadeIsNotWritten(t *testing.T) {
	uc, store, _, _, _ := newScriptedUseCase(nil, &fakeMask{failAt: errors.New("no values")})

	if _, err := uc.Ingest(context.Background(),
		IngestInput{Method: "GET", URL: "https://api.example.com"}); err == nil {
		t.Fatal("Ingest wrote down a capture it could not mask")
	}
	if len(store.saved) != 0 {
		t.Errorf("saved %d records, want none", len(store.saved))
	}
}

func TestListGetAndBody(t *testing.T) {
	uc, _, _, notifier := newUseCase()
	ctx := context.Background()

	if _, err := uc.Send(ctx, input()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Send is asynchronous; the record is readable once the send has finished.
	notifier.waitFor(t, TopicRequestFinished)

	rows := mustList(t, uc, domain.SourceManual)
	if len(rows) != 1 || rows[0].Method != "GET" {
		t.Fatalf("rows = %+v, want the sent request", rows)
	}
	// What a list carries is the record without its body text: the id is enough to ask for it.
	if rows[0].ResponseBody == nil || rows[0].ResponseBody.Size != int64(len(`{"data":[]}`)) {
		t.Errorf("responseBody = %+v, want its size and no text", rows[0].ResponseBody)
	}

	body, err := uc.Body(ctx, rows[0].ID, domain.SideResponse)
	if err != nil || body != `{"data":[]}` {
		t.Errorf("Body = %q, %v; want the response text", body, err)
	}

	if err := uc.Clear(ctx, []string{rows[0].ID}); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if rows := mustList(t, uc, ""); len(rows) != 0 {
		t.Errorf("after Clear: %+v", rows)
	}
}

func TestListDefaultsToWhatHistoryKeeps(t *testing.T) {
	uc, store, _, _ := newUseCase()

	if _, err := uc.List(context.Background(), "", 0); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(store.listed) != 1 || store.listed[0] != domain.HistoryKeepCount {
		t.Errorf("limit = %v, want %d", store.listed, domain.HistoryKeepCount)
	}
}

func TestPruneUsesTheRetentionWindow(t *testing.T) {
	uc, store, _, _ := newUseCase()

	if _, err := uc.Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(store.pruned) != 1 {
		t.Fatalf("pruned %d times, want once", len(store.pruned))
	}
	opts := store.pruned[0]
	if opts.MaxCount != domain.HistoryKeepCount {
		t.Errorf("maxCount = %d, want %d", opts.MaxCount, domain.HistoryKeepCount)
	}
	if opts.MaxAge != 7*24*time.Hour {
		t.Errorf("maxAge = %s, want the week the settings name", opts.MaxAge)
	}
}

func TestPruneRunsEverySoManySaves(t *testing.T) {
	uc, store, _, _ := newUseCase()
	ctx := context.Background()

	for i := 0; i < pruneEvery-1; i++ {
		rec := domain.Record{RecordSummary: domain.RecordSummary{ID: "rec-" + string(rune('a'+i))}}
		if err := uc.save(ctx, domain.WorkspacePersonalID, rec); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	if len(store.pruned) != 0 {
		t.Errorf("pruned %d times before the batch was full", len(store.pruned))
	}

	if err := uc.save(ctx, domain.WorkspacePersonalID,
		domain.Record{RecordSummary: domain.RecordSummary{ID: "rec-last"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(store.pruned) != 1 {
		t.Errorf("pruned %d times, want once at the end of the batch", len(store.pruned))
	}
}

func TestClearAllEmptiesTheWorkspaceOnScreen(t *testing.T) {
	uc, store, _, notifier := newUseCase()
	store.saved = []domain.Record{
		{RecordSummary: domain.RecordSummary{ID: "mine-1", WorkspaceID: domain.WorkspacePersonalID}},
		{RecordSummary: domain.RecordSummary{ID: "mine-2", WorkspaceID: domain.WorkspacePersonalID}},
		{RecordSummary: domain.RecordSummary{ID: "theirs", WorkspaceID: "elsewhere"}},
	}

	gone, err := uc.ClearAll(context.Background())
	if err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	if gone != 2 {
		t.Errorf("cleared %d records, want the two of the space on screen", gone)
	}
	if len(store.saved) != 1 || store.saved[0].ID != "theirs" {
		t.Errorf("left behind: %+v", store.saved)
	}
	if asked := store.askedIn[len(store.askedIn)-1]; asked != domain.WorkspacePersonalID {
		t.Errorf("cleared in %q, want the space the scope named", asked)
	}

	// The windows are told which history went: the list in the main window is drawn from rows that
	// are no longer in the database, and it has nothing else to go by.
	cleared, ok := notifier.waitFor(t, TopicHistoryCleared).(HistoryCleared)
	if !ok {
		t.Fatalf("the event carried the wrong payload")
	}
	if cleared.WorkspaceID != domain.WorkspacePersonalID || cleared.Removed != 2 {
		t.Errorf("the event said %+v, want the space and the two records", cleared)
	}
}

func TestHistoryWeighsTheWorkspaceOnScreen(t *testing.T) {
	uc, store, _, _ := newUseCase()
	store.saved = []domain.Record{
		{RecordSummary: domain.RecordSummary{ID: "with-body", WorkspaceID: domain.WorkspacePersonalID},
			ResponseBody: &domain.BodyRef{Size: 2048}},
		{RecordSummary: domain.RecordSummary{ID: "without", WorkspaceID: domain.WorkspacePersonalID}},
		{RecordSummary: domain.RecordSummary{ID: "elsewhere", WorkspaceID: "elsewhere"},
			ResponseBody: &domain.BodyRef{Size: 4096}},
	}

	stats, err := uc.History(context.Background())
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if stats.Count != 2 {
		t.Errorf("count = %d, want the two of the space on screen", stats.Count)
	}
	if stats.Bytes != 2048 {
		t.Errorf("bytes = %d, want only the bodies of that space", stats.Bytes)
	}
}

func mustList(t *testing.T, uc *UseCase, source domain.RecordSource) []domain.Record {
	t.Helper()
	rows, err := uc.List(context.Background(), source, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return rows
}

// The byte kinds travel to the prelude blanked — the scriptengine's business, tested there — so
// what this side owes is the other half: the prepared body is what goes on the wire.
func TestThePreparedBodyIsWhatGoesOut(t *testing.T) {
	for _, want := range []struct {
		kind domain.BodyKind
		body string
	}{
		{domain.BodyJSON, `{"a": 1}`},
		{domain.BodyRaw, "plain"},
		{domain.BodyForm, "multipart-form-bytes"},
		{domain.BodyBinary, "PNGDATA"},
	} {
		screen := &fakeScreen{}
		uc, _, executor, notifier, _ := newScriptedUseCase(screen, &fakeMask{})
		in := input()
		in.Body, in.BodyKind = want.body, want.kind

		if _, err := uc.Send(context.Background(), in); err != nil {
			t.Fatalf("Send: %v", err)
		}
		notifier.waitFor(t, TopicRequestFinished)

		if len(screen.passes) == 0 {
			t.Fatalf("%s: the scripts were never asked", want.kind)
		}
		if len(executor.got) != 1 {
			t.Fatalf("%s: the engine was asked %d times, want once", want.kind, len(executor.got))
		}
		if got := executor.got[0].Body; got != want.body {
			t.Errorf("%s: sent %q, want the prepared body %q", want.kind, got, want.body)
		}
	}
}

// A form whose scripts touched nothing is not masked a second time: the prepared copy already
// describes the request, and asking again would be a round trip for an answer nobody changed.
func TestAFormBodyIsNotMaskedAgainWhenNothingChanged(t *testing.T) {
	screen := &fakeScreen{}
	mask := &fakeMask{}
	uc, _, executor, notifier, _ := newScriptedUseCase(screen, mask)
	in := input()
	in.BodyKind = domain.BodyForm
	in.Body = `--BOUNDARY
Content-Disposition: form-data; name="token"

s3cret
--BOUNDARY--`
	in.MaskedBody = `--BOUNDARY
Content-Disposition: form-data; name="token"

••••
--BOUNDARY--`

	uc.Send(context.Background(), in)
	notifier.waitFor(t, TopicRequestFinished)

	if len(executor.got) != 1 {
		t.Fatalf("the engine was asked %d times, want once", len(executor.got))
	}
	if executor.got[0].Body != in.Body {
		t.Errorf("sent body = %q, want the prepared one", executor.got[0].Body)
	}
	if mask.asked != 0 {
		t.Errorf("the mask was asked %d times, want none — nothing changed", mask.asked)
	}
}

// A workspace is what the whole app is scoped to, and this feature resolves it once at the door:
// the id the scope answers with has to be the id the store is asked in, because a record written
// under a re-read pointer could land in a space the request never happened in.
func TestTheWorkspaceOnScreenReachesTheStore(t *testing.T) {
	const workspace = "team-1"
	uc, store, _, _, _ := newScopedUseCase(fakeScope{id: workspace}, nil, nil)
	ctx := context.Background()

	if _, err := uc.List(ctx, "", 0); err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, err := uc.Ingest(ctx,
		IngestInput{Method: "GET", URL: "https://api.example.com"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	// SendAndWait is told its space rather than reading the pointer, because its caller is a run that
	// resolved one when it started: naming a different one here has to be enough to move the record.
	if _, err := uc.SendAndWait(ctx, "other-space", input()); err != nil {
		t.Fatalf("SendAndWait: %v", err)
	}

	// One call per method above, and the first two in the workspace the scope named.
	want := []string{workspace, workspace, "other-space"}
	if len(store.askedIn) != len(want) {
		t.Fatalf("the store was addressed %d time(s), want one per call: %v", len(store.askedIn),
			store.askedIn)
	}
	for i, got := range store.askedIn {
		if got != want[i] {
			t.Errorf("call %d was made in %q, want %q", i, got, want[i])
		}
	}
}

// SendAndWait must not consult the scope at all: a run carries one space through every request it
// sends, and this is the half of that promise this package owns. A scope that would answer with
// something else — and a call that would fail on it — is never asked.
func TestSendAndWaitCarriesTheSpaceItWasGiven(t *testing.T) {
	uc, store, _, _, _ := newScopedUseCase(failingScope{}, nil, nil)

	if _, err := uc.SendAndWait(context.Background(), "run-space", input()); err != nil {
		t.Fatalf("SendAndWait: %v", err)
	}
	if len(store.askedIn) != 1 || store.askedIn[0] != "run-space" {
		t.Fatalf("the store was addressed %v, want exactly [run-space]", store.askedIn)
	}
}

// failingScope is a scope nobody may ask: every call is a failure, so a path that reaches for it
// says so instead of quietly landing somewhere.
type failingScope struct{}

func (failingScope) ActiveWorkspace(context.Context) (string, error) {
	return "", errors.New("the scope was asked for a workspace it should have been handed")
}

// A record of another device arrives as metadata; its body is fetched once, when opened, and kept.
func TestABodyOfSomebodyElsesRecordIsFetchedOnceAndKept(t *testing.T) {
	source := &fakeSource{fetched: domain.RecordFetched{
		Bodies: map[domain.BodySide]domain.BodyRef{
			domain.SideResponse: {Inline: `{"data":[]}`, Size: 11},
		},
	}}
	uc, store := newFetchingUseCase(source)
	store.saved = []domain.Record{remoteOf("rec-1")}
	ctx := context.Background()

	body, err := uc.Body(ctx, "rec-1", domain.SideResponse)
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	if body != `{"data":[]}` {
		t.Errorf("body = %q, want what the fetch answered", body)
	}
	if len(source.asked) != 1 || source.asked[0] != "rec-1" {
		t.Fatalf("asked %v, want the one record", source.asked)
	}

	// Opening it a second time is reading it: the body is here now, and a viewer that drew a record
	// twice would otherwise ask a server twice for the same bytes.
	if _, err := uc.Body(ctx, "rec-1", domain.SideResponse); err != nil {
		t.Fatalf("Body: %v", err)
	}
	if len(source.asked) != 1 {
		t.Errorf("asked %v, want the fetch to have happened once", source.asked)
	}
}

// A body is opened through the same call that brings the reports of the code around the record and
// the run it was sent by: all of it comes from one fetch, and all of it is handed to the store
// together — a body kept without the reports beside it would be a record half read.
func TestABodyIsKeptWithTheReportsAndTheRunBesideIt(t *testing.T) {
	run := domain.CollectionRun{
		ID: "run-1", CollectionID: "col-1", StartedAt: 10,
		Results: []domain.CollectionRunResult{{NodeID: "n-1", OK: true}},
	}
	source := &fakeSource{fetched: domain.RecordFetched{
		Bodies: map[domain.BodySide]domain.BodyRef{
			domain.SideResponse: {Inline: `{"data":[]}`, Size: 11},
		},
		Scripts: []domain.ScriptRun{{
			ID: "sr-1", Scope: domain.ScriptPost, OK: true,
			Logs:  []domain.ScriptLog{{Level: "log", Message: "sent"}},
			Tests: []domain.TestResult{{Name: "status is 200", Passed: true}},
		}},
		Run: &run,
	}}
	uc, store := newFetchingUseCase(source)
	store.saved = []domain.Record{remoteOf("rec-1")}

	if _, err := uc.Body(context.Background(), "rec-1", domain.SideResponse); err != nil {
		t.Fatalf("Body: %v", err)
	}
	if len(store.fetched) != 1 {
		t.Fatalf("kept %d fetches, want the one that was made", len(store.fetched))
	}
	kept := store.fetched[0]
	if len(kept.Scripts) != 1 || kept.Scripts[0].ID != "sr-1" {
		t.Errorf("scripts = %v, want the report that came with the body", kept.Scripts)
	}
	if len(kept.Scripts[0].Tests) != 1 || kept.Scripts[0].Tests[0].Name != "status is 200" {
		t.Errorf("tests = %v, want the assertion the report carried", kept.Scripts[0].Tests)
	}
	if kept.Run == nil || kept.Run.ID != "run-1" {
		t.Fatalf("run = %v, want the run the record names", kept.Run)
	}
	if len(kept.Run.Results) != 1 || kept.Run.Results[0].NodeID != "n-1" {
		t.Errorf("run results = %v, want the row the run produced", kept.Run.Results)
	}
}

// A record this device made carries its bodies with it, so one of its sides that is missing is one
// that was never there: there is nothing to fetch, and nothing is asked of the server.
func TestABodyOfThisDevicesOwnRecordIsNotFetched(t *testing.T) {
	source := &fakeSource{}
	uc, store := newFetchingUseCase(source)
	own := remoteOf("rec-1")
	own.IsRemote = false
	store.saved = []domain.Record{own}

	body, err := uc.Body(context.Background(), "rec-1", domain.SideResponse)
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	if body != "" {
		t.Errorf("body = %q, want nothing: the record never carried one", body)
	}
	if len(source.asked) != 0 {
		t.Errorf("asked %v, want a record of this device's own left alone", source.asked)
	}
}

// remoteOf is a record that came from somewhere else, which is what the fetch path is about.
func remoteOf(id string) domain.Record {
	return domain.Record{
		RecordSummary: domain.RecordSummary{
			ID: id, WorkspaceID: domain.WorkspacePersonalID, Source: domain.SourceManual,
			Method: "GET", URL: "https://api.test/users", Status: 200,
			IsRemote: true,
		},
	}
}

// newFetchingUseCase is a history whose records of other devices may be fetched: the source is the
// test's own, because what a fetch does — and how often — is what these tests are about.
func newFetchingUseCase(source Source) (*UseCase, *fakeStore) {
	store := newFakeStore()
	return NewUseCase(store, fakeScope{}, nil, nil, newFakeNotifier(),
		RetentionSourceFunc(func(context.Context) (domain.Retention, error) {
			return domain.RetainForever, nil
		}), source, nil, nil, platform.NewIDGen(),
		platform.BuildInfo{Name: "Schemo"}), store
}

func startedAt(id string, at int64) domain.Record {
	record := remoteOf(id)
	record.StartedAt = at
	return record
}

// A page that this device holds whole is read locally: scrolling through the window is not a call
// to the server.
func TestAPageThisDeviceHoldsIsNotAskedOfTheServer(t *testing.T) {
	source := &fakeSource{}
	uc, store := newFetchingUseCase(source)
	store.saved = []domain.Record{startedAt("a", 30), startedAt("b", 20), startedAt("c", 10)}

	page, err := uc.Page(context.Background(), 100, 2)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if len(page) != 2 || page[0].ID != "a" || page[1].ID != "b" {
		t.Errorf("page = %v, want a and b, newest first", page)
	}
	if len(source.olderFrom) != 0 {
		t.Errorf("the server was asked %v, want no call", source.olderFrom)
	}
}

// Past the window the rest is read from the server from where the local rows end, and kept as a
// cache so that scrolling back to it costs nothing.
func TestAPageThatRunsOutContinuesFromTheServerAndKeepsIt(t *testing.T) {
	source := &fakeSource{older: []domain.Record{startedAt("d", 8), startedAt("e", 6)}}
	uc, store := newFetchingUseCase(source)
	store.saved = []domain.Record{startedAt("a", 30), startedAt("b", 20)}

	page, err := uc.Page(context.Background(), 100, 4)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if len(source.olderFrom) != 1 || source.olderFrom[0] != 20 {
		t.Errorf("asked from %v, want from where the local rows end (20)", source.olderFrom)
	}
	if len(store.cached) != 2 {
		t.Errorf("cached %d records, want the 2 the server answered", len(store.cached))
	}
	if len(page) != 4 || page[3].ID != "e" {
		t.Errorf("page = %v, want the local rows and then the cached ones", page)
	}
}

// Scrolling back is a convenience: with the server out of reach the person still sees what this
// device has.
func TestAPageWithTheServerOutOfReachIsWhatThisDeviceHolds(t *testing.T) {
	source := &fakeSource{err: errors.New("offline")}
	uc, store := newFetchingUseCase(source)
	store.saved = []domain.Record{startedAt("a", 30)}

	page, err := uc.Page(context.Background(), 100, 5)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if len(page) != 1 {
		t.Errorf("page = %v, want the one local record", page)
	}
}

// A send an agent asked for is marked with its name, and one the person makes is not marked at all:
// that name is the one thing in the history that tells the agent's doing from the person's.
func TestASendCarriesTheAgentThatAskedForIt(t *testing.T) {
	uc, store, _, notifier := newUseCase()

	in := input()
	in.Agent = "Claude Desktop"
	if _, err := uc.Send(context.Background(), in); err != nil {
		t.Fatalf("Send: %v", err)
	}
	finished := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)
	if finished.Record.Agent != "Claude Desktop" {
		t.Errorf("agent = %q, want the name it was sent as", finished.Record.Agent)
	}
	if len(store.saved) != 1 || store.saved[0].Agent != "Claude Desktop" {
		t.Errorf("saved = %+v, want the name written down with the record", store.saved)
	}

	own := input()
	if _, err := uc.Send(context.Background(), own); err != nil {
		t.Fatalf("Send: %v", err)
	}
	mine := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)
	if mine.Record.Agent != "" {
		t.Errorf("agent = %q, want nothing for a send the person made", mine.Record.Agent)
	}
}
