package twitch

import (
	"io"
	"net/http"
)

// drainAndClose eats any unread response body and closes it. Always
// call on every response before returning, including error paths —
// http.Transport only reuses a keep-alive connection if the body is
// read to EOF.
//
// The 32 KB cap is plenty: every JSON error body Twitch emits is a
// few hundred bytes, and if somehow we hit a large body here it
// means the caller mis-parsed something and we're about to discard
// it anyway.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 32<<10)
	_ = resp.Body.Close()
}
