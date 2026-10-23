package app

import (
	"schemo/internal/agent"
	"schemo/internal/cloud"
	"schemo/internal/convert"
	"schemo/internal/desktop"
	"schemo/internal/request"
	"schemo/internal/script"
	"schemo/internal/workspace"
)

type ServicesIn struct {
	System       *System
	Account      *cloud.Account
	Settings     *workspace.Settings
	Updates      *desktop.Updates
	History      *request.History
	Drafts       *request.Drafts
	Environments *workspace.Environments
	Collections  *workspace.Collections
	Runs         *request.Runs
	Scripts      *script.Scripts
	Extension    *agent.Extension
	Workspaces   *workspace.Workspaces
	Search       *workspace.Search
	Inspector    *request.Inspector
	Cookies      *request.Cookies
	Sync         *cloud.Sync
	Members      *cloud.Members
	Plan         *cloud.Plan
	MCP          *agent.MCP
	Transfer     *convert.Transfer
	Publications *cloud.Publications
}
