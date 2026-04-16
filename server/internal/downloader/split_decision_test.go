package downloader

import (
	"errors"
	"fmt"
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

// TestIsSplitSignal pins the classification rule the outer part loop
// uses to decide "finalize this part and re-enter for a new one"
// vs. "hard-fail the job." Both signal shapes per spec §"Variant
// loss mid-stream" must classify as split; everything else must not.
//
// Wrapped errors must still match — the loop's caller wraps via
// fmt.Errorf("…: %w", err) before checking. A predicate that only
// matches bare sentinels would silently miss the real cases.
func TestIsSplitSignal(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "playlist gone (bare sentinel)",
			err:  hls.ErrPlaylistGone,
			want: true,
		},
		{
			name: "playlist gone (wrapped)",
			err:  fmt.Errorf("hls run: %w", hls.ErrPlaylistGone),
			want: true,
		},
		{
			name: "variant changed (bare sentinel)",
			err:  ErrVariantChanged,
			want: true,
		},
		{
			name: "variant changed (wrapped quality)",
			err:  fmt.Errorf("%w: quality %q → %q", ErrVariantChanged, "1080", "720"),
			want: true,
		},
		{
			name: "playlist auth (must NOT split — auth refresh path)",
			err:  hls.ErrPlaylistAuth,
			want: false,
		},
		{
			name: "permanent auth (must NOT split — entitlement)",
			err:  hls.ErrPlaylistAuthPermanent,
			want: false,
		},
		{
			name: "transport error (must NOT split — gap policy)",
			err:  errors.New("hls poller: status 500: server error"),
			want: false,
		},
		{
			name: "user cancel (must NOT split — terminal)",
			err:  ErrCancelled,
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isSplitSignal(tc.err)
			if got != tc.want {
				t.Errorf("isSplitSignal(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
