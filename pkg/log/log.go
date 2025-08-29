package log

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
	"os"
)

// Logger defines a structured logging interface that wraps zap.Logger
type Logger interface {
	Debug(msg string, fields ...zap.Field)
	Info(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
	With(fields ...zap.Field) Logger
}

// zapLogger implements Logger interface wrapping zap.Logger
type zapLogger struct {
	*zap.Logger
}

func (z *zapLogger) Debug(msg string, fields ...zap.Field) {
	z.Logger.Debug(msg, fields...)
}

func (z *zapLogger) Info(msg string, fields ...zap.Field) {
	z.Logger.Info(msg, fields...)
}

func (z *zapLogger) Warn(msg string, fields ...zap.Field) {
	z.Logger.Warn(msg, fields...)
}

func (z *zapLogger) Error(msg string, fields ...zap.Field) {
	z.Logger.Error(msg, fields...)
}

func (z *zapLogger) With(fields ...zap.Field) Logger {
	return &zapLogger{z.Logger.With(fields...)}
}

// NewLogger creates a new Logger wrapping the provided zap.Logger
func NewLogger(z *zap.Logger) Logger {
	if z == nil {
		// Create a no-op logger if nil is provided
		return &zapLogger{zap.NewNop()}
	}
	return &zapLogger{z}
}

// NewDevelopmentLogger creates a development logger with console output to stdout
func NewDevelopmentLogger() Logger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.OutputPaths = []string{"stdout"}      // Output to stdout for containers
	config.ErrorOutputPaths = []string{"stderr"} // Errors still to stderr

	logger, err := config.Build()
	if err != nil {
		// Fallback to a simple logger if config fails
		logger = zap.New(zapcore.NewCore(
			zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
			zapcore.AddSync(os.Stdout), // Changed from os.Stderr
			zapcore.DebugLevel,
		))
	}

	return NewLogger(logger)
}

// noopLogger implements Logger interface but does nothing
type noopLogger struct{}

func (n *noopLogger) Debug(msg string, fields ...zap.Field) {}
func (n *noopLogger) Info(msg string, fields ...zap.Field)  {}
func (n *noopLogger) Warn(msg string, fields ...zap.Field)  {}
func (n *noopLogger) Error(msg string, fields ...zap.Field) {}
func (n *noopLogger) With(fields ...zap.Field) Logger       { return n }

// NewNopLogger creates a logger that does nothing - useful for tests
func NewNopLogger() Logger {
	return &noopLogger{}
}

// NewTestLogger creates a logger that writes to the given writer - useful for tests
func NewTestLogger(writer io.Writer) Logger {
	config := zap.NewDevelopmentEncoderConfig()
	config.EncodeLevel = zapcore.LowercaseLevelEncoder
	config.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(config),
		zapcore.AddSync(writer),
		zapcore.DebugLevel,
	)

	logger := zap.New(core)
	return NewLogger(logger)
}

// NewProductionLogger creates a production logger with JSON output to stdout
func NewProductionLogger() Logger {
	config := zap.NewProductionConfig()
	config.OutputPaths = []string{"stdout"}      // Output to stdout for containers
	config.ErrorOutputPaths = []string{"stderr"} // Errors still to stderr

	logger, err := config.Build()
	if err != nil {
		// Fallback to a simple logger if config fails
		logger = zap.New(zapcore.NewCore(
			zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
			zapcore.AddSync(os.Stdout), // Changed from os.Stderr
			zapcore.InfoLevel,
		))
	}

	return NewLogger(logger)
}
