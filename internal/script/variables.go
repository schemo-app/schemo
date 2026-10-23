package script

import (
	"cmp"
	"context"
	"schemo/internal/domain"
	"slices"
)

// maxRuns is how many runs keep a scope at once. Nothing reads a run's own memory once the run is
// over, so a session of a thousand sends does not have to keep a thousand of them alive for the
// life of the process.
const maxRuns = 32

type scriptVariables struct {
	ctx   context.Context
	owner *Scripts
	run   string
}

// Lookup answers the run's own scope first, then the environment, then the globals, which is the
// order a request resolves in, so a script gets what the next request of the run will get. Asking
// for either of the other two reads that one alone, which is how a script tells where a name is
// set.
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

// Set writes where the scope says it goes. A value a script puts in the run's own scope is this
// run's alone: the next request of the same run sees it, another run does not, and a restart
// forgets it. The other two are stored, which is their point; a secret written there is stored like
// any other value, because the app has nowhere else to keep it.
func (v scriptVariables) Set(scope domain.VarScope, name string, value string) error {
	if scope != domain.ScopeRun {
		err := v.owner.vars.SetVariable(v.ctx, scope, name, value)
		return v.owner.keptForTheRun(v.run, name, value, err)
	}
	v.owner.setRunVariable(v.run, name, value)
	return nil
}

// keptForTheRun turns the refusal of a workspace this person may only read into the run's own scope: a
// script that records a token for the next request still works, and what the workspace holds is not
// changed. Any other answer is the store's and goes back as it came.
func (s *Scripts) keptForTheRun(run, name, value string, err error) error {
	if domain.CodeOf(err) != domain.CodeWorkspaceReadOnly {
		return err
	}
	s.setRunVariable(run, name, value)
	return nil
}

// RunVariables is what the scripts of a run keep for it alone, as the variables its next request is
// resolved with. They stand innermost, where a value set for one run overrides the workspace's own.
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
