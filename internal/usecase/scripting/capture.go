package scripting

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"schemo/internal/domain"
	"schemo/internal/lens"
)

// applyCaptures runs a level's capture rules on the response and reports them, or reports nothing
// when none is on. A rule that finds nothing fails alone; the rest still run.
func (u *UseCase) applyCaptures(
	ctx context.Context,
	at domain.ScriptPass,
	level Level,
) (domain.ScriptRun, bool) {
	if at.Response == nil || at.Response.Error != "" || at.Response.Cancelled {
		return domain.ScriptRun{}, false
	}
	started := time.Now()
	report := domain.ScriptRun{
		ID: u.ids(), RecordID: at.RecordID, NodeID: level.NodeID, Scope: domain.ScriptPost,
		OK: true, Logs: []domain.ScriptLog{}, Tests: []domain.TestResult{},
	}
	ran := false
	for _, rule := range level.Scripts.Captures {
		if rule.Off {
			continue
		}
		ran = true
		began := time.Now()
		result := domain.TestResult{Name: describe(rule), Passed: true}
		value, err := extract(at.Response, rule)
		if err == nil {
			err = u.keep(ctx, at, rule, value)
		}
		if err != nil {
			result.Passed, result.Error = false, err.Error()
			report.OK = false
		} else {
			report.Logs = append(report.Logs, domain.ScriptLog{
				Level: "info", Message: rule.Name + " = " + shown(rule, value),
			})
		}
		result.DurationUs = time.Since(began).Microseconds()
		report.Tests = append(report.Tests, result)
	}
	if !ran {
		return domain.ScriptRun{}, false
	}
	report.DurationUs = time.Since(started).Microseconds()
	report.CreatedAt = time.Now().UnixMilli()
	return report, true
}

// keep stores a captured value; secrets go through CaptureVariable, which the script store lacks.
func (u *UseCase) keep(
	ctx context.Context,
	at domain.ScriptPass,
	rule domain.Capture,
	value string,
) error {
	if rule.Scope != domain.ScopeRun && rule.Secret {
		err := u.vars.CaptureVariable(ctx, rule.Scope, rule.Name, value, true)
		return u.keptForTheRun(at.Run, rule.Name, value, err)
	}
	return scriptVariables{ctx: ctx, owner: u, run: at.Run}.Set(rule.Scope, rule.Name, value)
}

// describe names a rule in a report.
func describe(rule domain.Capture) string {
	return rule.Name + " ← " + where(rule)
}

func where(rule domain.Capture) string {
	switch rule.Source {
	case domain.CaptureStatus:
		return "status"
	case domain.CaptureHeader:
		return "header " + rule.Path
	case domain.CaptureCookie:
		return "cookie " + rule.Path
	}
	return rule.Path
}

// shown is a value as a report shows it; a secret is never written.
func shown(rule domain.Capture, value string) string {
	if rule.Secret {
		return "••••"
	}
	const most = 120
	if len([]rune(value)) > most {
		return string([]rune(value)[:most]) + "…"
	}
	return value
}

// extract reads the rule's value from the response.
func extract(response *domain.Response, rule domain.Capture) (string, error) {
	switch rule.Source {
	case domain.CaptureStatus:
		return strconv.Itoa(response.Status), nil
	case domain.CaptureHeader:
		for _, h := range response.Headers {
			if strings.EqualFold(h.Name, rule.Path) {
				return h.Value, nil
			}
		}
		return "", fmt.Errorf("the response has no header %s", rule.Path)
	case domain.CaptureCookie:
		return cookieValue(response, rule.Path)
	case domain.CaptureBody:
		doc, err := lens.Open(response.ContentType, response.Body)
		if err != nil {
			return "", fmt.Errorf("the body is not a document that can be read")
		}
		value, err := doc.Text(rule.Path)
		if err != nil {
			return "", fmt.Errorf("nothing is at %s", rule.Path)
		}
		return value, nil
	}
	return "", fmt.Errorf("unknown source %q", rule.Source)
}

// cookieValue finds a cookie the response set.
func cookieValue(response *domain.Response, name string) (string, error) {
	for _, cookie := range domain.ParseResponseCookies(response.Headers, time.Now()) {
		if cookie.Name == name {
			return cookie.Value, nil
		}
	}
	return "", fmt.Errorf("the response sets no cookie %s", name)
}
