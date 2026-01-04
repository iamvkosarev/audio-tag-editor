package logs

import (
	"log/slog"
)

func Error(message string, err error, attr ...slog.Attr) {
	slog.Error(message, slog.String("err", err.Error()), attr)
}
