package logging

import (
	"gopkg.in/natefinch/lumberjack.v2"
	"log/slog"
	"os"
)

func New(path string) *slog.Logger {
	w := &lumberjack.Logger{Filename: path, MaxSize: 50, MaxBackups: 5, MaxAge: 14, Compress: true}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
func Stdout() *slog.Logger { return slog.New(slog.NewJSONHandler(os.Stdout, nil)) }
