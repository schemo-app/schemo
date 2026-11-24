// Package request owns request preparation, transport and history.
package request

import (
	"context"
	"slices"
	"time"

	"schemo/internal/domain"
)

// Send starts a request and returns its id; the answer arrives as an event, so it can be cancelled
// and a slow endpoint does not hold the call open.
//
//wails:ignore
func (s *History) Send(ctx context.Context, in SendInput) (string, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", err
	}
	return s.send(ctx, workspace, in)
}

func (s *History) send(ctx context.Context, workspace string, in SendInput) (string, error) {
	// Wails ends the start call as soon as it receives the attempt's id.
	attemptCtx, active, err := s.startAttempt(context.WithoutCancel(ctx), workspace, in)
	if err != nil {
		return "", err
	}
	go func() {
		defer s.finishAttempt(active)
		if _, err := s.runAndAnnounce(attemptCtx, active, in); err != nil {
			s.notifier.Publish(TopicRequestFailed, RequestFailed{
				ID:      active.id,
				Error:   err.Error(),
				Failure: domain.AsFailure(err),
			})
		}
	}()

	return active.id, nil
}

// SendAndWait is Send for callers that need the answer first; a cancellation is not an error. The
// workspace is the caller's, so a run files every answer under one workspace.
//
//wails:ignore
func (s *History) SendAndWait(
	ctx context.Context,
	workspace string,
	in SendInput,
) (domain.Record, error) {
	attemptCtx, active, err := s.startAttempt(ctx, workspace, in)
	if err != nil {
		return domain.Record{}, err
	}
	defer s.finishAttempt(active)
	return s.runAndAnnounce(attemptCtx, active, in)
}

// runAndAnnounce reports how an attempt ended, including cancellation before a record was written.
func (s *History) runAndAnnounce(
	ctx context.Context,
	active *attempt,
	in SendInput,
) (domain.Record, error) {
	rec, err := s.executeAttempt(ctx, active, in)
	if err != nil {
		return domain.Record{}, err
	}
	s.notifier.Publish(TopicRequestFinished, RequestFinished{ID: active.id, Record: rec})
	return rec, nil
}

// attempt runs one request to the end. An error here is this side failing: a transport failure
// is the engine's to report inside the response.
func (s *History) executeAttempt(
	ctx context.Context,
	active *attempt,
	in SendInput,
) (domain.Record, error) {
	workspace, id, started := active.workspace, active.id, active.started
	senderID, err := s.senderID(ctx)
	if err != nil {
		return domain.Record{}, err
	}
	cancelled := domain.Record{
		RecordSummary: domain.RecordSummary{ID: id, ProtocolRecord: domain.ProtocolRecord{Protocol: in.Protocol}},
		Cancelled:     true,
	}
	if ctx.Err() != nil {
		return cancelled, nil
	}
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

	if s.screen != nil {
		skip, err := s.screen.Before(ctx, workspace, &pass)
		if ctx.Err() != nil {
			return cancelled, nil
		}
		if err != nil {
			return domain.Record{}, err
		}
		if skip {
			// Nothing was sent: return the attempt's shape so the caller learns it was stopped, not failed.
			return domain.Record{
				RecordSummary: domain.RecordSummary{
					ProtocolRecord: domain.ProtocolRecord{Protocol: in.Protocol},
					ID:             id,
					Source:         domain.SourceManual,
					Method:         pass.Request.Method,
					URL:            in.MaskedURL,
					StartedAt:      started,
				},
				Skipped: true,
			}, nil
		}
	}

	masked := s.masked(ctx, workspace, in, pass.Request)
	if stream := active.stream; stream != nil {
		stream.opened = func(response *domain.Response) {
			record := s.recordFrom(id, workspace, started, in, pass, masked, response)
			record.SenderID = senderID
			record.Active = true
			stream.setRecord(record)
			s.notifier.Publish(TopicStreamOpened, RequestFinished{ID: id, Record: record})
		}
	}

	resp, err := s.executor.Execute(ctx, Request{
		Protocol: in.Protocol, rpc: in.rpc, Stream: active.stream,
		ID:      id,
		Method:  pass.Request.Method,
		URL:     pass.Request.URL,
		Headers: pass.Request.Headers,
		Body:    pass.Request.Body,
		Digest:  in.Digest,
		// The jar the engine reads and writes on the way is this workspace's: a redirect is a second
		// request, and the workspace is what the two hops have in common.
		Workspace: workspace,
	})
	if err != nil {
		return domain.Record{}, err
	}
	if resp.Cancelled {
		return cancelled, nil
	}
	if active.stream != nil {
		// A disconnect still owes history its completed transcript.
		var finish context.CancelFunc
		ctx, finish = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finish()
	}

	rec := s.recordFrom(id, workspace, started, in, pass, masked, resp)
	rec.SenderID = senderID
	if err := s.save(ctx, workspace, rec); err != nil {
		return domain.Record{}, err
	}

	// The second half of the pass runs after the record is written and not before it: its reports hang
	// off that record, and the reports of the first half are carried in the pass for the same reason.
	if s.screen != nil {
		pass.Response = resp
		s.screen.After(ctx, workspace, pass)
	}
	s.offer(ctx, workspace, rec.ID)
	return rec, nil
}

// masked is the request as history keeps it. If masking fails the unmasked copy is not stored, since
// that would write a secret in clear.
func (s *History) masked(ctx context.Context, workspace string, in SendInput, sent *domain.ScriptRequest) Masked {
	prepared := Masked{URL: in.MaskedURL, Headers: in.MaskedHeaders, Body: in.MaskedBody}
	if s.mask == nil || !changed(in, sent) {
		return prepared
	}
	again, err := s.mask.Mask(ctx, workspace, in, *sent)
	if err != nil {
		return prepared
	}
	return again
}

// runScope is the scope of `pm.variables`: the run, or the send itself when it has none.
func runScope(run string, id string) string {
	if run != "" {
		return run
	}
	return id
}

// changed is whether a script rewrote the request after it was prepared.
func changed(in SendInput, sent *domain.ScriptRequest) bool {
	return in.URL != sent.URL || in.Body != sent.Body || !slices.Equal(in.Headers, sent.Headers)
}

// Cancel stops an attempt by id, reporting whether anything was still running under it.
func (s *History) Cancel(id string) bool {
	if active := s.findAttempt(id); active != nil {
		active.cancel()
		return true
	}
	return false
}

// requestNode names the source request; strip drafts are not collection requests, so they name none.
func requestNode(node string) string {
	if domain.IsTabDraft(domain.DraftID(node)) {
		return ""
	}
	return node
}

// sentHeaders is the masked rows plus the engine's own, such as a Digest answer computed after preparing.
func sentHeaders(masked []domain.HeaderPair, resp *domain.Response) []domain.HeaderPair {
	return append(domain.OrEmpty(masked), resp.SentHeaders...)
}

func (s *History) recordFrom(
	id string,
	workspace string,
	started int64,
	in SendInput,
	pass domain.ScriptPass,
	masked Masked,
	resp *domain.Response,
) domain.Record {
	body, options, operation := recordRequest(masked.Body, in)
	return domain.Record{
		RecordSummary: domain.RecordSummary{
			ProtocolRecord: domain.ProtocolRecord{
				Protocol: in.Protocol, Options: options, MessageCount: resp.MessageCount,
				Operation: operation,
			},
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
		// Jar writes from every hop; the engine applied them as it went.
		CookieWrites: domain.OrEmpty(resp.CookieWrites),
		RequestBody:  bodyRef(body, false),
		ResponseBody: bodyRef(resp.Body, resp.BodyTruncated),
	}
}
