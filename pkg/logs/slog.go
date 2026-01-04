package logs

import (
	"errors"
	"io"
	"log/slog"
	"os"
)

const (
	Debug = "debug"
	Dev   = "dev"
	Prod  = "prod"
)

func NewSlogLogger(mode string, prodWriter io.Writer) (*slog.Logger, error) {
	var th slog.Handler
	switch mode {
	case Debug:
		th = slog.NewTextHandler(
			os.Stdout, &slog.HandlerOptions{
				Level:       slog.LevelDebug,
				ReplaceAttr: replaceAttr(slog.LevelDebug),
			},
		)
	case Dev:
		th = slog.NewJSONHandler(
			os.Stdout, &slog.HandlerOptions{
				Level:       slog.LevelInfo,
				ReplaceAttr: replaceAttr(slog.LevelInfo),
			},
		)
	case Prod:
		th = slog.NewJSONHandler(
			prodWriter, &slog.HandlerOptions{
				Level:       slog.LevelError,
				ReplaceAttr: replaceAttr(slog.LevelError),
			},
		)
	default:
		return nil, errors.New("invalid logging mode")
	}
	return slog.New(th), nil
}

func replaceAttr(level slog.Level) func(groups []string, a slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		if level == slog.LevelDebug {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			if a.Key == slog.LevelKey || a.Value.Any().(string) == slog.LevelInfo.String() {
				return slog.Attr{}
			}
		}

		handleCustomLevel(&a)
		return a
	}
}
