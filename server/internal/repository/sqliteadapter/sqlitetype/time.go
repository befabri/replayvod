package sqlitetype

import (
	"database/sql/driver"
	"fmt"
	"time"
)

const layout = "2006-01-02 15:04:05"

// Time scans SQLite TEXT timestamps for sqlc rows/params. Malformed non-NULL
// values return errors; NULL scans to zero for optional LEFT JOIN projections.
type Time struct {
	time.Time
}

func NewTime(t time.Time) Time {
	return Time{Time: t.UTC()}
}

func Format(t time.Time) string {
	return t.UTC().Format(layout)
}

func Parse(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("sqlite timestamp: unparseable empty value")
	}
	if t, err := time.Parse(layout, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("sqlite timestamp: unparseable value %q", s)
}

func (t *Time) Scan(src any) error {
	if src == nil {
		t.Time = time.Time{}
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		t.Time = v.UTC()
		return nil
	case string:
		parsed, err := Parse(v)
		if err != nil {
			return err
		}
		t.Time = parsed
		return nil
	case []byte:
		parsed, err := Parse(string(v))
		if err != nil {
			return err
		}
		t.Time = parsed
		return nil
	default:
		return fmt.Errorf("sqlite timestamp: cannot scan %T", src)
	}
}

func (t Time) Value() (driver.Value, error) {
	return Format(t.Time), nil
}
