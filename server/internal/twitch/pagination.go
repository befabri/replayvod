package twitch

import (
	"errors"
	"fmt"
)

// ErrPaginationStalled reports a Helix listing that cannot make progress: the
// cursor came back unchanged, or the page cap was reached. Without it a drain
// loop would call the API until the process died.
var ErrPaginationStalled = errors.New("twitch: pagination stalled")

// maxPaginationPages bounds the generated *All helpers. Followed-channel and
// EventSub listings legitimately span many pages, so the cap only catches a
// runaway.
const maxPaginationPages = 1000

// CheckPageCursor decides whether a drain loop may fetch the page after page
// (1-based). previous is the cursor that fetched the current page and next the
// non-empty cursor Helix returned with it.
func CheckPageCursor(previous, next string, page, maxPages int) error {
	if next == previous {
		return fmt.Errorf("%w: cursor %q repeated on page %d", ErrPaginationStalled, next, page)
	}
	if page >= maxPages {
		return fmt.Errorf("%w: page cap %d reached", ErrPaginationStalled, maxPages)
	}
	return nil
}
