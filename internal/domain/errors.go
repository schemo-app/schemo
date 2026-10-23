package domain

import (
	"errors"
	"slices"
	"strings"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")

	// ErrStaleRevision is returned when a text buffer or row patch is based on a revision the draft
	// has moved past. The caller gets the current draft back and re-sends its text, so no keystroke
	// is dropped.
	ErrStaleRevision = errors.New("stale revision")

	// ErrNotAllowed marks an operation the current state forbids, as opposed to one that is
	// merely malformed — the UI phrases the two differently.
	ErrNotAllowed = errors.New("not allowed")

	// ErrUnreadable marks a body that is not written in any notation the inspector reads. It is an
	// answer and not a failure: most bodies are text, and text has no positions to inspect.
	ErrUnreadable = errors.New("not a readable document")
)

type Code string

const (
	CodeNotFound             Code = "notFound"
	CodeNotAllowed           Code = "notAllowed"
	CodeConflict             Code = "conflict"
	CodeStaleRevision        Code = "staleRevision"
	CodeNameEmpty            Code = "nameEmpty"
	CodeDomainEmpty          Code = "domainEmpty"
	CodeNameTooLong          Code = "nameTooLong"
	CodeNameTaken            Code = "nameTaken"
	CodeDescriptionTooLong   Code = "descriptionTooLong"
	CodeFileMissing          Code = "fileMissing"
	CodeFileUnreadable       Code = "fileUnreadable"
	CodeFileTooLarge         Code = "fileTooLarge"
	CodeBadCertificate       Code = "badCertificate"
	CodeNotJSON              Code = "notJson"
	CodeNotPostman           Code = "notPostmanCollection"
	CodeNotSchemo            Code = "notSchemoCollection"
	CodeNotOpenAPI           Code = "notOpenAPI"
	CodeNotInsomnia          Code = "notInsomniaExport"
	CodeNotHAR               Code = "notHAR"
	CodeNotCommand           Code = "notCommand"
	CodeURLUnreachable       Code = "urlUnreachable"
	CodeNothingToImport      Code = "nothingToImport"
	CodeImportExpired        Code = "importExpired"
	CodeSkippedCookieJar     Code = "skippedCookieJar"
	CodeSkippedExternalRef   Code = "skippedExternalRef"
	CodeSkippedBase64Body    Code = "skippedBase64Body"
	CodeSkippedBinaryFile    Code = "skippedBinaryFile"
	CodeSkippedSecretValue   Code = "skippedSecretValue"
	CodeSkippedAuthScheme    Code = "skippedAuthScheme"
	CodeRunInProgress        Code = "runInProgress"
	CodeNodeHasNoRequests    Code = "nodeHasNoRequests"
	CodeNoEnvironment        Code = "noEnvironment"
	CodeUnknownField         Code = "unknownField"
	CodeUnknownFormat        Code = "unknownFormat"
	CodeUnknownList          Code = "unknownList"
	CodeDraftWithoutID       Code = "draftWithoutId"
	CodeIntoItself           Code = "intoItself"
	CodeUnknownTheme         Code = "unknownTheme"
	CodeUnknownLanguage      Code = "unknownLanguage"
	CodeUnknownListSide      Code = "unknownListSide"
	CodeUnknownRetention     Code = "unknownRetention"
	CodeUnknownChannel       Code = "unknownUpdateChannel"
	CodeUnknownShortcut      Code = "unknownShortcut"
	CodeUnknownPort          Code = "unknownPort"
	CodeUnknownProxyMode     Code = "unknownProxyMode"
	CodeUnknownHTTPVersion   Code = "unknownHttpVersion"
	CodeUnknownProxyProtocol Code = "unknownProxyProtocol"
	CodeBadProxyAddress      Code = "badProxyAddress"
	CodeNoVersion            Code = "noVersion"
	CodeUpdateFromImage      Code = "updateFromImage"
	CodeUpdateReadOnly       Code = "updateReadOnly"
	CodeDevBuild             Code = "devBuild"
	CodeWorkspacePermanent   Code = "workspacePermanent"
	CodeWorkspaceMissing     Code = "workspaceMissing"
	CodeMemberExists         Code = "memberExists"
	CodeFeatureRequired      Code = "featureRequired"
	CodeWorkspacePersonal    Code = "workspacePersonal"
	CodeVariableSecret       Code = "variableSecret"
	CodeEnvironmentReadOnly  Code = "environmentReadOnly"
	CodeWorkspaceReadOnly    Code = "workspaceReadOnly"
	CodeVariableMissing      Code = "variableMissing"
	CodeNotSignedIn          Code = "notSignedIn"
	CodeServerUnreachable    Code = "serverUnreachable"
	CodeSessionEnded         Code = "sessionEnded"
	CodeServerRefused        Code = "serverRefused"
	CodeSignInFailed         Code = "signInFailed"

	// What a collection this device will not publish is refused with: a page carries the author's
	// words to strangers, so what is drawn on it is held to what a page can say.
	CodeTextTooLong         Code = "textTooLong"
	CodeURLTooLong          Code = "urlTooLong"
	CodeURLControlChar      Code = "urlControlChar"
	CodeFoldersTooDeep      Code = "foldersTooDeep"
	CodeTooManyItems        Code = "tooManyItems"
	CodeMethodUnsupported   Code = "methodUnsupported"
	CodeProtocolUnsupported Code = "protocolUnsupported"
	CodeBodyTypeUnsupported Code = "bodyTypeUnsupported"
	CodeStatusOutOfRange    Code = "statusOutOfRange"
	// CodePasswordInvalid is a password that is not one a page can stand behind. The bounds travel
	// with it, and they are the server's: this side refuses what the server would, before it is sent.
	CodePasswordInvalid Code = "passwordInvalid"
	// CodeSnapshotVersion is a page's document written by a build this one does not read. It is the
	// refusal the server makes of the same bytes, made here because they are already in hand.
	CodeSnapshotVersion Code = "snapshotVersion"
)

type Args map[string]string

type Failure struct {
	Code Code `json:"code"`
	Args Args `json:"args,omitempty"`

	sentinel error
}

func (f *Failure) Error() string {
	if len(f.Args) == 0 {
		return string(f.Code)
	}
	keys := make([]string, 0, len(f.Args))
	for key := range f.Args {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+f.Args[key])
	}
	return string(f.Code) + "(" + strings.Join(parts, ", ") + ")"
}

func (f *Failure) Unwrap() error { return f.sentinel }

func Refuse(code Code, sentinel error, args Args) error {
	return &Failure{Code: code, Args: args, sentinel: sentinel}
}

func AsFailure(err error) *Failure {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure
	}
	return nil
}

// FailureOf converts an error into a Failure: the code and the values its sentence needs. An error
// that is not ours never reached a server, so the caller supplies fallback.
func FailureOf(err error, fallback Code) *Failure {
	if failure := AsFailure(err); failure != nil {
		return failure
	}
	return &Failure{Code: fallback}
}

var sentinelCodes = []struct {
	err  error
	code Code
}{
	{ErrNotFound, CodeNotFound},
	{ErrNotAllowed, CodeNotAllowed},
	{ErrConflict, CodeConflict},
	{ErrStaleRevision, CodeStaleRevision},
}

func CodeOf(err error) Code {
	if failure := AsFailure(err); failure != nil {
		return failure.Code
	}
	for _, entry := range sentinelCodes {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	return ""
}

// Notifier is where a feature publishes what the window should be told: a topic and a payload. It
// is declared once here instead of in each feature that needs one.
type Notifier interface {
	Publish(topic string, payload any)
}

// TopicChanged is the one event a window refreshes on: something of Kind changed, in Workspace when
// the kind belongs to one. The window reloads what it shows of that kind instead of reading a
// payload, so every feature announces changes the same way.
const TopicChanged = "changed"

type Changed struct {
	Kind      string `json:"kind"`
	Workspace string `json:"workspace,omitempty"`
}
