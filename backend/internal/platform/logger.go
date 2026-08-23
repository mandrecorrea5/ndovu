// Package platform reúne infraestrutura transversal (logger estruturado).
package platform

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger cria o logger estruturado (JSON) da aplicação.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
