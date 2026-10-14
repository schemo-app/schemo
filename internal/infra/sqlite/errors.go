package sqlite

import (
	"errors"

	sqlite "modernc.org/sqlite"
	"modernc.org/sqlite/lib"
)

func isUniqueViolation(err error) bool {
	var sqliteError *sqlite.Error
	if !errors.As(err, &sqliteError) {
		return false
	}
	return sqliteError.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
		sqliteError.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}
