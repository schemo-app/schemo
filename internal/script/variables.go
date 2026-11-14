package script

import (
	"cmp"
	"context"
	"schemo/internal/domain"
	"slices"
)

// maxRuns is how many run scopes are kept; a finished run's scope is never read again.
const maxRuns = 32

type scriptVariables struct {
	ctx   context.Context
	owner *Scripts
	run   string
}

// Lookup searches the run scope, the environment, then the globals, as a request resolves; a named
// scope reads only that one.
func (v scriptVariables) Lookup(scope domain.VarScope, name string) (string, bool, error) {
	if scope != domain.ScopeRun {
		return v.owner.vars.Variable(v.ctx, scope, name)
	}
	if value, ok := v.owner.runVariable(v.run, name); ok {
		return value, true, nil
	}
	value, ok, err := v.owner.vars.Variable(v.ctx, domain.ScopeEnvironment, name)
	if err != nil || ok {
		return value, ok, err
	}
	return v.owner.vars.Variable(v.ctx, domain.ScopeGlobals, name)
}

// Set writes to the scope given. Run-scope values last for the run only; the other scopes are stored.
func (v scriptVariables) Set(scope domain.VarScope, name string, value string) error {
	if scope != domain.ScopeRun {
		err := v.owner.vars.SetVariable(v.ctx, scope, name, value)
		return v.owner.keptForTheRun(v.run, name, value, err)
	}
	v.owner.setRunVariable(v.run, name, value)
	return nil
}

// keptForTheRun turns a read-only workspace's refusal into a run-scope write, so a script saving a
// token still works; other errors pass through.
func (s *Scripts) keptForTheRun(run, name, value string, err error) error {
	if domain.CodeOf(err) != domain.CodeWorkspaceReadOnly {
		return err
	}
	s.setRunVariable(run, name, value)
	return nil
}

// RunVariables are a run's script-set variables, innermost when resolving its next request.
//
//wails:ignore
func (s *Scripts) RunVariables(run string) []domain.Variable {
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := make([]domain.Variable, 0, len(s.runs[run]))
	for name, value := range s.runs[run] {
		kept = append(kept, domain.Variable{
			Name: name, Value: value, Kind: domain.VariableText, Enabled: true, HasValue: value != "",
		})
	}
	slices.SortFunc(kept, func(a, b domain.Variable) int { return cmp.Compare(a.Name, b.Name) })
	return kept
}

func (s *Scripts) runVariable(run string, name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, ok := s.runs[run][name]
	return value, ok
}

func (s *Scripts) setRunVariable(run string, name string, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.runs[run] == nil {
		// Which entry goes is not worth knowing: a run that is over never reads its scope again.
		// What matters is that the entry of the run being written is never the one taken.
		if len(s.runs) >= maxRuns {
			for held := range s.runs {
				if held != run {
					delete(s.runs, held)
					break
				}
			}
		}
		s.runs[run] = map[string]string{}
	}
	s.runs[run][name] = value
}
