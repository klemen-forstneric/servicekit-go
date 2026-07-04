// Package log provides a slog-backed ember.LoggerCtx that stamps the
// correlation id from the context onto every line.
package log

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/klemen-forstneric/ember"
	"github.com/klemen-forstneric/ember/correlation"
)

// Logger adapts slog to ember.LoggerCtx.
type Logger struct {
	l *slog.Logger
}

// New returns a JSON logger writing to stdout.
func New() *Logger { return newWithWriter(os.Stdout) }

func newWithWriter(w io.Writer) *Logger {
	return &Logger{l: slog.New(slog.NewJSONHandler(w, nil))}
}

func (g *Logger) log(ctx context.Context, level slog.Level, msg string, kvs ...any) {
	if cid, err := correlation.FromContext(ctx); err == nil {
		kvs = append([]any{"correlation_id", cid}, kvs...)
	}
	g.l.Log(ctx, level, msg, kvs...)
}

func (g *Logger) Debug(ctx context.Context, msg string, kvs ...any) {
	g.log(ctx, slog.LevelDebug, msg, kvs...)
}

func (g *Logger) Info(ctx context.Context, msg string, kvs ...any) {
	g.log(ctx, slog.LevelInfo, msg, kvs...)
}

func (g *Logger) Warn(ctx context.Context, msg string, kvs ...any) {
	g.log(ctx, slog.LevelWarn, msg, kvs...)
}

func (g *Logger) Error(ctx context.Context, msg string, err error, kvs ...any) {
	g.log(ctx, slog.LevelError, msg, append([]any{"error", err}, kvs...)...)
}

var _ ember.LoggerCtx = (*Logger)(nil)
