package sqlite

import (
	"errors"
	"fmt"
	"testing"
)

func TestConstraintErrorsAreClassifiedByDriverCode(t *testing.T) {
	store := newMigratedStore(t)
	ctx := t.Context()
	if _, err := store.DB().ExecContext(ctx, `CREATE TABLE constraint_fixture (
		id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, enabled INTEGER CHECK (enabled IN (0, 1)));
		INSERT INTO constraint_fixture VALUES (1, 'first', 1)`); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, query string
		conflict    bool
	}{
		{"primary key", `INSERT INTO constraint_fixture VALUES (1, 'second', 0)`, true},
		{"unique", `INSERT INTO constraint_fixture VALUES (2, 'first', 0)`, true},
		{"check", `INSERT INTO constraint_fixture VALUES (2, 'second', 2)`, false},
		{"not null", `INSERT INTO constraint_fixture VALUES (2, NULL, 1)`, false},
		{"foreign key", `INSERT INTO record_bodies (record_id, side) VALUES ('missing', 'request')`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := store.DB().ExecContext(ctx, scenario.query)
			if err == nil {
				t.Fatal("constraint violation was accepted")
			}
			if got := isUniqueViolation(fmt.Errorf("operation: %w", err)); got != scenario.conflict {
				t.Fatalf("conflict = %v, want %v: %v", got, scenario.conflict, err)
			}
		})
	}
	if isUniqueViolation(nil) || isUniqueViolation(errors.New("UNIQUE constraint failed")) {
		t.Fatal("a non-driver error was classified by its text")
	}
}
