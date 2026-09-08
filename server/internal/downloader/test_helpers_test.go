package downloader

import "log/slog"

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }
