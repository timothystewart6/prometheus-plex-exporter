package plex

import (
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is a minimal structured logger used by the package.
// Method signatures match the previous implementation so call sites remain unchanged.
type Logger interface {
	Info(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
	Debug(msg string, fields ...zap.Field)
}

// zapLogger is the single logger implementation for the package.
type zapLogger struct {
	logger *zap.Logger
}

// NewLogger creates a zap-backed Logger that writes to the provided io.Writer.
// If out is nil, os.Stderr is used.
func NewLogger(out io.Writer) Logger {
	return NewLoggerWithLevel(out, zapcore.InfoLevel)
}

// NewLoggerWithLevel creates a zap-backed Logger writing to the provided io.Writer
// using the provided zapcore.Level as the minimum enabled level. If out is nil,
// os.Stderr is used.
func NewLoggerWithLevel(out io.Writer, level zapcore.Level) Logger {
	if out == nil {
		out = os.Stderr
	}

	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.RFC3339TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	encoder := zapcore.NewJSONEncoder(encoderCfg)
	ws := zapcore.AddSync(out)
	core := zapcore.NewCore(encoder, ws, level)

	logger := zap.New(core)

	return &zapLogger{logger: logger}
}
func (z *zapLogger) Info(msg string, fields ...zap.Field)  { z.logger.Info(msg, fields...) }
func (z *zapLogger) Warn(msg string, fields ...zap.Field)  { z.logger.Warn(msg, fields...) }
func (z *zapLogger) Error(msg string, fields ...zap.Field) { z.logger.Error(msg, fields...) }
func (z *zapLogger) Debug(msg string, fields ...zap.Field) { z.logger.Debug(msg, fields...) }

// package default logger: single zap-based logger
var logger Logger = NewLogger(os.Stderr)

// SetLogger replaces the package-level logger. Passing nil resets to the default zap logger.
func SetLogger(l Logger) {
	if l == nil {
		logger = NewLogger(os.Stderr)
		return
	}
	logger = l
}
