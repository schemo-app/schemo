package record

import (
	"context"
	"time"

	"schemo/internal/domain"
)

// Send starts a request and returns its id at once. The answer arrives as an event: it has to
// outlive the call that started it, because it can be cancelled and because a slow endpoint must
// not hold the window's promise open.
func (u *UseCase) Send(ctx context.Context, in SendInput) (string, error) {
	// The workspace is read here and carried into the goroutine as a value: the attempt outlives
	// this call, and asking again down there would write the answer into whatever space the user
	// had switched to by the time it arrived.
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", err
	}

	id := u.ids()
	started := time.Now().UnixMilli()

	// The attempt runs on its own context: the caller's ends when the frontend call returns, and a
	// request must not be cancelled by the act of asking for it.
	go func() {
		rec, err := u.attempt(context.Background(), workspace, id, started, in)
		if err != nil {
			u.notifier.Publish(TopicRequestFailed, RequestFailed{
				ID:      id,
				Error:   err.Error(),
				Failure: domain.AsFailure(err),
			})
			return
		}
		if rec.Cancelled {
			// An attempt the user stopped has nothing to keep: history holds what came back, and
			// nothing did. The window stopped it, so it already knows.
			return
		}
		u.notifier.Publish(TopicRequestFinished, RequestFinished{ID: id, Record: rec})
	}()

	return id, nil
}

// SendAndWait is Send for a caller that has to see the answer before it does the next thing. A
// cancelled attempt comes back saying so rather than as an error — it was stopped on purpose.
//
// The workspace is the caller's, not this method's: a run resolves it once and carries it through
// every request it sends, and asking again here would file the twentieth answer under another
// space.
func (u *UseCase) SendAndWait(
	ctx context.Context,
	workspace string,
	in SendInput,
) (domain.Record, error) {
	id := u.ids()
	rec, err := u.attempt(ctx, workspace, id, time.Now().UnixMilli(), in)
	if err != nil {
		return domain.Record{}, err
	}
	if rec.Cancelled {
		return rec, nil
	}
	u.notifier.Publish(TopicRequestFinished, RequestFinished{ID: id, Record: rec})
	return rec, nil
}

// attempt runs one request to the end. An error here is this side failing: a transport failure
// is the engine's to report inside the response.
func (u *UseCase) attempt(
	ctx context.Context,
	workspace, id string,
	started int64,
	in SendInput,
) (domain.Record, error) {
	pass := domain.ScriptPass{
		// The scripts of a run share one scope, and a request sent on its own is a run of one: what
		// the record keeps is the run it names, which for a send on its own is none.
		Run:      runScope(in.Run, id),
		RecordID: id,
		NodeID:   in.Node,
		Request: &domain.ScriptRequest{
			Method:   in.Method,
			URL:      in.URL,
			Headers:  in.Headers,
			Body:     in.Body,
			BodyKind: in.BodyKind,
		},
	}

	if u.screen != nil {
		skip, err := u.screen.Before(ctx, workspace, &pass)
		if err != nil {
			return domain.Record{}, err
		}
		if skip {
			// Nothing goes out, so there is no answer to fold in and nothing for history to keep.
			// What is left is the shape of the attempt — which request it was, and that it was not
			// sent — so whoever asked learns that rather than that a request failed.
			return domain.Record{
				RecordSummary: domain.RecordSummary{
					ID:        id,
					Source:    domain.SourceManual,
					Method:    pass.Request.Method,
					URL:       in.MaskedURL,
					StartedAt: started,
				},
				Skipped: true,
			}, nil
		}
	}

	resp, err := u.executor.Execute(ctx, Request{
		ID:      id,
		Method:  pass.Request.Method,
		URL:     pass.Request.URL,
		Headers: pass.Request.Headers,
		Body:    pass.Request.Body,
		Digest:  in.Digest,
		// The jar the engine reads and writes on the way is this space's: a redirect is a second
		// request, and the workspace is what the two hops have in common.
		Workspace: workspace,
	})
	if err != nil {
		return domain.Record{}, err
	}
	if resp.Cancelled {
		return domain.Record{Cancelled: true}, nil
	}

	rec := u.recordFrom(id, workspace, started, in, pass, u.masked(ctx, in, pass.Request), resp)
	if err := u.save(ctx, workspace, rec); err != nil {
		return domain.Record{}, err
	}

	// The second half of the pass runs after the record is written and not before it: its reports hang
	// off that record, and the reports of the first half are carried in the pass for the same reason.
	if u.screen != nil {
		pass.Response = resp
		u.screen.After(ctx, workspace, pass)
	}
	u.offer(ctx, workspace, rec.ID)
	return rec, nil
}

// masked is the request as history keeps it. What the caller prepared is the answer until a script
// changes the request — the mask is made where the values are, and that is not here.
//
// A mask that cannot be made leaves the prepared one in place: the unusable answer is the masked
// one, because the other would be a secret written down in the clear.
func (u *UseCase) masked(ctx context.Context, in SendInput, sent *domain.ScriptRequest) Masked {
	prepared := Masked{URL: in.MaskedURL, Headers: in.MaskedHeaders, Body: in.MaskedBody}
	if u.mask == nil || !changed(in, sent) {
		return prepared
	}
	again, err := u.mask.Mask(ctx, in, *sent)
	if err != nil {
		return prepared
	}
	return again
}

// runScope is where this request's `pm.variables` live: the run it belongs to, or — for a request
// sent on its own, from the command line or from a card — the send itself. Every request has a
// scope; only a run shares one between requests.
func runScope(run string, id string) string {
	if run != "" {
		return run
	}
	return id
}

func changed(in SendInput, sent *domain.ScriptRequest) bool {
	if in.URL != sent.URL || in.Body != sent.Body || len(in.Headers) != len(sent.Headers) {
		return true
	}
	for i, header := range in.Headers {
		if sent.Headers[i] != header {
			return true
		}
	}
	return false
}

// Cancel stops an attempt by id, reporting whether anything was still running under it.
func (u *UseCase) Cancel(id string) bool {
	return u.executor.Cancel(id)
}

// requestNode is the request of a collection this send came from, which is what makes that
// request's history its own sends. A strip draft is not a request of any collection — it is where a
// request is composed rather than where one is kept — and a record of it names no node rather than
// naming a draft.
func requestNode(node string) string {
	if domain.IsTabDraft(domain.DraftID(node)) {
		return ""
	}
	return node
}

// sentHeaders is what the record keeps: the masked rows, plus what the engine had to compute
// itself — a Digest answer comes from a challenge that had not arrived when the request was
// prepared, and the engine hands that one over already masked.
func sentHeaders(masked []domain.HeaderPair, resp *domain.Response) []domain.HeaderPair {
	return append(domain.OrEmpty(masked), resp.SentHeaders...)
}

func (u *UseCase) recordFrom(
	id string,
	workspace string,
	started int64,
	in SendInput,
	pass domain.ScriptPass,
	masked Masked,
	resp *domain.Response,
) domain.Record {
	return domain.Record{
		RecordSummary: domain.RecordSummary{
			ID:          id,
			WorkspaceID: workspace,
			Source:      domain.SourceManual,
			Method:      pass.Request.Method,
			URL:         masked.URL,
			NodeID:      requestNode(in.Node),
			RunID:       in.Run,
			Agent:       in.Agent,
			Status:      resp.Status,
			StatusText:  resp.StatusText,
			ContentType: resp.ContentType,
			Error:       resp.Error,
			DurationUs:  resp.DurationUs,
			StartedAt:   started,
		},
		Cancelled:       resp.Cancelled,
		DNSUs:           resp.DNSUs,
		ConnectUs:       resp.ConnectUs,
		TLSUs:           resp.TLSUs,
		WaitUs:          resp.WaitUs,
		DownloadUs:      resp.DownloadUs,
		RequestBytes:    int64(len(pass.Request.Body)),
		ResponseBytes:   int64(len(resp.Body)),
		RequestHeaders:  sentHeaders(masked.Headers, resp),
		ResponseHeaders: domain.OrEmpty(resp.Headers),
		// What the exchange did to the jar, every hop of it. The engine knows the hops and asks the
		// jar as it goes — a cookie one answer set is a cookie the next request carries — so what is
		// left here is to keep what came back.
		CookieWrites: domain.OrEmpty(resp.CookieWrites),
		RequestBody:  bodyRef(masked.Body, false),
		ResponseBody: bodyRef(resp.Body, resp.BodyTruncated),
	}
}
