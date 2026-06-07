package sqliteadapter

import (
	"database/sql"
	"encoding/json"
)

// rawMessageFromSQLite converts a nullable TEXT column holding JSON into a
// json.RawMessage, returning nil for SQL NULL. Mirrors the inline conversion
// the hand-written mappers used before generation.
func rawMessageFromSQLite(v sql.NullString) json.RawMessage {
	if !v.Valid {
		return nil
	}
	return json.RawMessage(v.String)
}
