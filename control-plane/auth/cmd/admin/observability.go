package main

import (
	"io"
	"log/slog"
	"strings"
)

func newComponentLogger(component string, output io.Writer, levelText string) *slog.Logger {
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(strings.TrimSpace(levelText))); err != nil {
		level = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})).With("component", component)
}
