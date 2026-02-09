package logger

import (
	"context"
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

// Init initializes the global structured logger
func Init(level slog.Level, format string) {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	}

	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// Get returns the default logger
func Get() *slog.Logger {
	if defaultLogger == nil {
		Init(slog.LevelInfo, "text")
	}
	return defaultLogger
}

// WithContext returns a logger with context values
func WithContext(ctx context.Context) *slog.Logger {
	logger := Get()

	// Extract common context values if they exist
	if traceID, ok := ctx.Value("trace_id").(string); ok {
		logger = logger.With("trace_id", traceID)
	}
	if requestID, ok := ctx.Value("request_id").(string); ok {
		logger = logger.With("request_id", requestID)
	}

	return logger
}

// Info logs at Info level
func Info(msg string, args ...any) {
	Get().Info(msg, args...)
}

// Error logs at Error level
func Error(msg string, args ...any) {
	Get().Error(msg, args...)
}

// Warn logs at Warn level
func Warn(msg string, args ...any) {
	Get().Warn(msg, args...)
}

// Debug logs at Debug level
func Debug(msg string, args ...any) {
	Get().Debug(msg, args...)
}

// InfoContext logs at Info level with context
func InfoContext(ctx context.Context, msg string, args ...any) {
	WithContext(ctx).Info(msg, args...)
}

// ErrorContext logs at Error level with context
func ErrorContext(ctx context.Context, msg string, args ...any) {
	WithContext(ctx).Error(msg, args...)
}

// WarnContext logs at Warn level with context
func WarnContext(ctx context.Context, msg string, args ...any) {
	WithContext(ctx).Warn(msg, args...)
}

// DebugContext logs at Debug level with context
func DebugContext(ctx context.Context, msg string, args ...any) {
	WithContext(ctx).Debug(msg, args...)
}
