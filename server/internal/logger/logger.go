package logger

import (
	"io"
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
)

var (
	currentLevel = slog.LevelDebug
	logFile      *os.File
)

func init() {
	handler := tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: "15:04:05.000",
	})
	slog.SetDefault(slog.New(handler))
}

// Close releases any resources held by the logger (e.g., open log file).
// Call this during graceful shutdown.
func Close() {
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}

// ParseLogLevel converts a string log level to slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}

// SetupLogger configures the global logger with console and optional file output.
func SetupLogger(output io.Writer, serviceName string, logToFile bool, logDir string, sampleRate float64) *slog.Logger {
	return SetupLoggerWithLevel(output, serviceName, logToFile, logDir, sampleRate, slog.LevelDebug)
}

// SetupLoggerWithLevel configures the global logger with a specific log level.
func SetupLoggerWithLevel(output io.Writer, serviceName string, logToFile bool, logDir string, sampleRate float64, level slog.Level) *slog.Logger {
	currentLevel = level

	var handlers []slog.Handler

	if logToFile {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			panic(err)
		}

		logPath := logDir + "/" + serviceName + ".log"
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			panic(err)
		}
		logFile = file

		handlerOptions := &slog.HandlerOptions{Level: level}
		handlers = append(handlers, slog.NewJSONHandler(file, handlerOptions))
	}

	handlers = append(handlers, tint.NewHandler(output, &tint.Options{
		Level:      level,
		TimeFormat: "15:04:05.000",
	}))

	logger := slog.New(slog.NewMultiHandler(handlers...))
	logger = logger.With("service", serviceName)

	slog.SetDefault(logger)
	return logger
}
