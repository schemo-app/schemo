package sqlite

import (
	"encoding/json"
	"reflect"
	"testing"

	"schemo/internal/domain"
)

// The rules a level keeps for its responses are part of what the level says, so they travel with it
// and come back the way they went: a device that applied them would run what the other wrote.
func TestCapturesTravelWithTheirLevel(t *testing.T) {
	written := domain.Scripts{Post: "pm.test('ok', () => {})", Captures: []domain.Capture{
		{ID: "c1", Name: "token", Source: domain.CaptureBody, Path: "$.access_token",
			Scope: domain.ScopeEnvironment, Secret: true},
		{ID: "c2", Name: "code", Source: domain.CaptureStatus, Scope: domain.ScopeRun, Off: true},
	}}
	raw, err := json.Marshal(written)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	data := scriptsData(string(raw))
	if data == nil || len(data.GetCaptures()) != 2 {
		t.Fatalf("scriptsData = %+v, want both rules on the wire", data)
	}
	stored, ok := scriptsJSON(data).(string)
	if !ok {
		t.Fatalf("scriptsJSON = %T, want a stored document", scriptsJSON(data))
	}
	var back domain.Scripts
	if err := json.Unmarshal([]byte(stored), &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, written) {
		t.Errorf("came back as %+v, want %+v", back, written)
	}
}

// A level whose only content is a rule is still a level with something to say.
func TestALevelWithOnlyCapturesIsNotEmpty(t *testing.T) {
	raw, _ := json.Marshal(domain.Scripts{Captures: []domain.Capture{
		{ID: "c1", Name: "code", Source: domain.CaptureStatus, Scope: domain.ScopeRun},
	}})
	if scriptsData(string(raw)) == nil {
		t.Error("a level with only a capture was sent as nothing")
	}
}
