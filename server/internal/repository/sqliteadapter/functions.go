package sqliteadapter

import (
	"database/sql/driver"
	"fmt"
	"strings"

	"modernc.org/sqlite"
)

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("unicode_lower", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if len(args) == 0 || args[0] == nil {
			return nil, nil
		}
		switch v := args[0].(type) {
		case string:
			return strings.ToLower(v), nil
		case []byte:
			return strings.ToLower(string(v)), nil
		default:
			return strings.ToLower(fmt.Sprint(v)), nil
		}
	})
}
