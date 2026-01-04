package logs

import (
	"log/slog"
)

const (
	LevelPanic     = slog.Level(10)
	LevelEmergency = slog.Level(12)
)

func handleCustomLevel(a *slog.Attr) {
	if a.Key == slog.LevelKey {
		level := a.Value.Any().(slog.Level)

		switch level {
		case LevelEmergency:
			a.Value = slog.StringValue("EMERGENCY")
		case LevelPanic:
			a.Value = slog.StringValue("PANIC")
		default:
			a.Value = slog.StringValue(level.String())
		}
	}
}
