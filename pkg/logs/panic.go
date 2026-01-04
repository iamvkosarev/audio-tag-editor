package logs

import (
	"context"
	"log/slog"
	"runtime"
)

func Panic(ctx context.Context, message string, panicValue any, attr ...slog.Attr) {
	const size = 64 << 10
	stack := make([]byte, size)
	stack = stack[:runtime.Stack(stack, false)]

	slog.Log(
		ctx, LevelPanic, message, slog.Any("panic", panicValue), slog.String("stack", string(stack)),
		attr,
	)
}
