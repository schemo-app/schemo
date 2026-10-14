package domain

import "regexp"

type CaptureSource string

const (
	CaptureBody   CaptureSource = "body"
	CaptureHeader CaptureSource = "header"
	CaptureCookie CaptureSource = "cookie"
	CaptureStatus CaptureSource = "status"
)

type Capture struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Source CaptureSource `json:"source"`
	Path   string        `json:"path,omitempty"`
	Scope  VarScope      `json:"scope"`
	Secret bool          `json:"secret,omitempty"`
	Off    bool          `json:"off,omitempty"`
}

var variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func IsVariableName(name string) bool { return variableName.MatchString(name) }

func (c Capture) Valid() bool {
	if !IsVariableName(c.Name) {
		return false
	}
	switch c.Scope {
	case ScopeRun, ScopeEnvironment, ScopeGlobals:
	default:
		return false
	}
	switch c.Source {
	case CaptureStatus:
		return true
	case CaptureBody, CaptureHeader, CaptureCookie:
		return c.Path != ""
	}
	return false
}
