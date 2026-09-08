package downloader

// failedRunTruncated reports whether a failed run stopped before the broadcast
// did. A run that finalized no part captured nothing and is not truncated; an
// unreadable part count counts as captured, so doubt shows as truncated.
func failedRunTruncated(partsKnown, hasPart, cutShort bool) bool {
	return (!partsKnown || hasPart) && cutShort
}
