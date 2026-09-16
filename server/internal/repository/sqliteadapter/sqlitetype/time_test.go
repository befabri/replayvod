package sqlitetype

import (
	"database/sql/driver"
	"fmt"
	"testing"
	"time"
)

func TestTimeScan(t *testing.T) {
	tests := []struct {
		name string
		src  any
		want time.Time
	}{
		{
			name: "sqlite layout",
			src:  "2026-04-12 15:30:45",
			want: time.Date(2026, 4, 12, 15, 30, 45, 0, time.UTC),
		},
		{
			name: "rfc3339 layout",
			src:  "2026-04-12T15:30:45Z",
			want: time.Date(2026, 4, 12, 15, 30, 45, 0, time.UTC),
		},
		{
			name: "precise layout keeps its nanoseconds",
			src:  "2026-04-12 15:30:45.123456789",
			want: time.Date(2026, 4, 12, 15, 30, 45, 123456789, time.UTC),
		},
		{
			name: "nil is zero for optional projection",
			src:  nil,
			want: time.Time{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Time
			if err := got.Scan(tt.src); err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("Scan() = %v, want %v", got.Time, tt.want)
			}
		})
	}
}

func TestTimeScanMalformedHardFails(t *testing.T) {
	tests := []any{"", "not-a-timestamp"}
	for _, src := range tests {
		t.Run(fmt.Sprintf("%#v", src), func(t *testing.T) {
			var got Time
			if err := got.Scan(src); err == nil {
				t.Fatal("Scan() error = nil, want malformed timestamp error")
			}
		})
	}
}

func TestTimeValueFormatsUTC(t *testing.T) {
	in := Time{Time: time.Date(2026, 6, 3, 12, 0, 0, 0, time.FixedZone("offset", 3600))}
	got, err := in.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	var _ driver.Value = got
	if got != "2026-06-03 11:00:00" {
		t.Fatalf("Value() = %q, want UTC SQLite layout", got)
	}
}

func TestPreciseTimeValueKeepsNanoseconds(t *testing.T) {
	in := NewPreciseTime(time.Date(2026, 6, 3, 12, 0, 0, 123456789, time.FixedZone("offset", 3600)))
	got, err := in.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	if got != "2026-06-03 11:00:00.123456789" {
		t.Fatalf("Value() = %q, want UTC precise layout", got)
	}
	var back PreciseTime
	if err := back.Scan(got); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if !back.Equal(in.Time.Time) {
		t.Fatalf("round trip = %v, want %v", back.Time.Time, in.Time.Time)
	}
	if plain, _ := NewTime(in.Time.Time).Value(); !(plain.(string) < got.(string)) {
		t.Fatalf("precise %q must sort after the same second %q", got, plain)
	}
}
