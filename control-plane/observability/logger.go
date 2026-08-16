// Package observability is the small helper module that every Go binary in
// the AstraSync control plane uses to install a JSON logger that records the
// component name as a structured field. The convention is documented at
// docs/observability/log-conventions.md.
package observability

import (
	"log/slog"
	"os"
)

// NewComponentLogger returns a JSON logger that tags every record with the
// supplied component name. The default level is INFO; the deployment can
// override the level by setting the LOG_LEVEL environment variable to one of
// DEBUG, INFO, WARN, or ERROR.
func NewComponentLogger(component string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(os.Getenv("LOG_LEVEL")),
	})).With("component", component)
}

func parseLevel(value string) slog.Level {
	switch value {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}