// Package logger implements the small structured logger used by the
// application. Output is one line per event in a logfmt-ish format that is
// easy for Docker to capture on stdout/stderr:
//
//	2026-09-25T21:30:15+05:30 INFO server started address=:8080
package logger

import (
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"
)

// Log levels.
const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = [...]string{"DEBUG", "INFO", "WARN", "ERROR"}

// Logger writes synchronized, single-line, human readable log events.
// It is a thin wrapper around the standard library log.Logger (which
// serializes writes) with the custom line format above.
type Logger struct {
	out   *log.Logger
	level int
}

// New returns a Logger writing to out at the given level
// ("debug", "info", "warn" or "error"; default "info").
func New(out io.Writer, level string) *Logger {
	return &Logger{
		out:   log.New(out, "", 0), // no date prefix, we format ourselves
		level: levelFromString(level),
	}
}

func levelFromString(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Debug logs a debug-level message.
func (l *Logger) Debug(msg string, kv ...any) { l.Write(LevelDebug, msg, kv...) }

// Info logs an informational message.
func (l *Logger) Info(msg string, kv ...any) { l.Write(LevelInfo, msg, kv...) }

// Warn logs a warning.
func (l *Logger) Warn(msg string, kv ...any) { l.Write(LevelWarn, msg, kv...) }

// Error logs an error.
func (l *Logger) Error(msg string, kv ...any) { l.Write(LevelError, msg, kv...) }

// Write logs a message at an explicit numeric level.
func (l *Logger) Write(level int, msg string, kv ...any) {
	if level < l.level {
		return
	}
	var b strings.Builder
	b.WriteString(time.Now().Format(time.RFC3339))
	b.WriteByte(' ')
	b.WriteString(levelNames[level])
	b.WriteByte(' ')
	b.WriteString(msg)
	for i := 0; i+1 < len(kv); i += 2 {
		key := fmt.Sprint(kv[i])
		val := fmt.Sprint(kv[i+1])
		if val == "" {
			val = `""`
		}
		if strings.ContainsAny(val, " \t\r\n") {
			val = strconv.Quote(val)
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(val)
	}
	b.WriteByte('\n')
	_ = l.out.Output(3, b.String())
}
