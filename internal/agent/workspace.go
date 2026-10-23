package agent

import (
	"context"
	"errors"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"schemo/internal/domain"
)

// WorkspaceTools delegates edits to the same application operations as the desktop UI.
type WorkspaceTools interface {
	Environments(context.Context, WorkspaceInput) (domain.EnvState, error)
	GetCollection(context.Context, IDInput) (CollectionDetails, error)
	CreateCollection(context.Context, CollectionInput) (IDOutput, error)
	UpdateCollection(context.Context, CollectionPatch) (IDOutput, error)
	DeleteCollection(context.Context, IDInput) (IDOutput, error)
	MoveCollection(context.Context, MoveInput) (IDOutput, error)
	CreateRequest(context.Context, RequestInput) (IDOutput, error)
	UpdateRequest(context.Context, RequestPatch) (IDOutput, error)
	DeleteRequest(context.Context, IDInput) (IDOutput, error)
	MoveRequest(context.Context, MoveInput) (IDOutput, error)
	CreateEnvironment(context.Context, EnvironmentInput) (IDOutput, error)
	UpdateEnvironment(context.Context, EnvironmentPatch) (IDOutput, error)
	DeleteEnvironment(context.Context, IDInput) (IDOutput, error)
	SetVariable(context.Context, VariableInput) (IDOutput, error)
	DeleteVariable(context.Context, VariableIDInput) (IDOutput, error)
}

type WorkspaceInput struct {
	Workspace string `json:"workspace" jsonschema:"workspace id returned by workspaces; does not switch the desktop workspace"`
}

func (in WorkspaceInput) workspace() string { return in.Workspace }

type IDInput struct {
	WorkspaceInput
	ID string `json:"id" jsonschema:"id of the target in this workspace"`
}

type IDOutput struct {
	ID string `json:"id"`
}

type CollectionDetails struct {
	Collection Collection `json:"collection"`
	Requests   []Request  `json:"requests"`
}

type CollectionInput struct {
	WorkspaceInput
	Name        string `json:"name"`
	Description string `json:"description,omitempty" jsonschema:"Markdown description"`
	Parent      string `json:"parent,omitempty" jsonschema:"parent folder id; omit for the workspace root"`
}

type CollectionPatch struct {
	IDInput
	Name        *string      `json:"name,omitempty"`
	Description *string      `json:"description,omitempty" jsonschema:"Markdown description; omitted fields remain unchanged"`
	Auth        *domain.Auth `json:"auth,omitempty" jsonschema:"replace saved authorization; omit to preserve it; use type inherit or none to change inheritance"`
}

type MoveInput struct {
	IDInput
	Parent   string `json:"parent" jsonschema:"destination collection id; empty is allowed only for a collection moved to the root"`
	Position int64  `json:"position" jsonschema:"zero-based position in the destination"`
}

// RequestFields addresses saved request data, without scripts or local file paths.
type RequestFields struct {
	Name        *string      `json:"name,omitempty"`
	Method      *string      `json:"method,omitempty"`
	URL         *string      `json:"url,omitempty"`
	Description *string      `json:"description,omitempty"`
	Headers     *[]Header    `json:"headers,omitempty" jsonschema:"replace enabled headers; omit to preserve all existing headers"`
	Params      *[]Header    `json:"params,omitempty" jsonschema:"replace query parameters"`
	Body        *string      `json:"body,omitempty"`
	BodyKind    *string      `json:"bodyKind,omitempty" jsonschema:"raw, json or xml; file bodies are not accepted"`
	Environment *string      `json:"environment,omitempty" jsonschema:"environment id to pin; empty clears the pin"`
	Auth        *domain.Auth `json:"auth,omitempty" jsonschema:"replace saved authorization; omit to preserve it; use variable references for protected credentials"`
}

type RequestInput struct {
	WorkspaceInput
	Collection string `json:"collection"`
	RequestFields
}

type RequestPatch struct {
	IDInput
	RequestFields
}

type EnvironmentInput struct {
	WorkspaceInput
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type EnvironmentPatch struct {
	IDInput
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

type VariableIDInput struct {
	WorkspaceInput
	Environment string `json:"environment,omitempty" jsonschema:"environment id; omit for workspace globals"`
	ID          string `json:"id"`
}

type VariableInput struct {
	WorkspaceInput
	Environment string `json:"environment,omitempty" jsonschema:"environment id; omit for workspace globals"`
	Name        string `json:"name"`
	Value       string `json:"value"`
	Secret      bool   `json:"secret,omitempty" jsonschema:"store as a secret; its value is never returned to MCP"`
}

// A workspace is explicit on every edit; a guessed id cannot escape the allowed scope.
func addWorkspaceTool[I interface{ workspace() string }, O any](
	s *MCP, name, description string, write, destructive bool, run func(context.Context, I) (O, error),
) {
	closed := false
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: name, Description: description,
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: !write, DestructiveHint: &destructive, OpenWorldHint: &closed},
	}, func(ctx context.Context, req *sdk.CallToolRequest, in I) (*sdk.CallToolResult, O, error) {
		var zero O
		policy := s.opened(req)
		if write && !policy.Write {
			return nil, zero, errors.New("editing is disabled in MCP settings")
		}
		if err := s.requireWorkspace(ctx, policy, in.workspace()); err != nil {
			return nil, zero, err
		}
		out, err := run(ctx, in)
		if write {
			// Some multi-step edits may have committed part of an edit before an error.
			s.notifier.Publish(TopicDataChanged, in.workspace())
		}
		return nil, out, err
	})
}

const TopicDataChanged = "mcp:data-changed"

func (s *MCP) registerWorkspaceReads() {
	addWorkspaceTool(s, "environments", "Read environments, variables and the active environment of a workspace. Secret values are withheld.", false, false, s.tools.Environments)
	addWorkspaceTool(s, "get_collection", "Read a collection or folder, its description, saved authorization and request list. Secret variable values are withheld.", false, false, s.tools.GetCollection)
}

var writeToolNames = []string{
	"create_collection", "update_collection", "delete_collection", "move_collection",
	"create_request", "update_request", "delete_request", "move_request",
	"create_environment", "update_environment", "delete_environment", "set_variable", "delete_variable",
}

func (s *MCP) setWriteTools(allowed bool) {
	if !allowed {
		s.mcp.RemoveTools(writeToolNames...)
		return
	}
	addWorkspaceTool(s, "create_collection", "Create a collection or nested folder in a workspace.", true, false, s.tools.CreateCollection)
	addWorkspaceTool(s, "update_collection", "Edit a collection's name, description or saved authorization. Omitted fields are preserved.", true, false, s.tools.UpdateCollection)
	addWorkspaceTool(s, "delete_collection", "Delete a collection and all its nested folders and requests.", true, true, s.tools.DeleteCollection)
	addWorkspaceTool(s, "move_collection", "Move or reorder a collection within the same workspace.", true, false, s.tools.MoveCollection)
	addWorkspaceTool(s, "create_request", "Save a new HTTP request in a collection. Does not send it. Use {{variables}} for credentials.", true, false, s.tools.CreateRequest)
	addWorkspaceTool(s, "update_request", "Edit a saved HTTP request and its authorization without sending it. Omitted fields and scripts are preserved. Never send masked values back.", true, false, s.tools.UpdateRequest)
	addWorkspaceTool(s, "delete_request", "Delete a saved HTTP request.", true, true, s.tools.DeleteRequest)
	addWorkspaceTool(s, "move_request", "Move or reorder a saved request in a collection of the same workspace.", true, false, s.tools.MoveRequest)
	addWorkspaceTool(s, "create_environment", "Create an environment in a workspace; it becomes that workspace's active environment.", true, false, s.tools.CreateEnvironment)
	addWorkspaceTool(s, "update_environment", "Rename an environment or change its color. Does not replace variables.", true, false, s.tools.UpdateEnvironment)
	addWorkspaceTool(s, "delete_environment", "Delete an environment and its variables.", true, true, s.tools.DeleteEnvironment)
	addWorkspaceTool(s, "set_variable", "Create or replace a variable by name in an environment or workspace globals. Existing secret variables remain secret.", true, false, s.tools.SetVariable)
	addWorkspaceTool(s, "delete_variable", "Delete a variable from an environment or workspace globals.", true, true, s.tools.DeleteVariable)
}
