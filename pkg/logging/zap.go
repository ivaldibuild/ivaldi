package logging

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// zapLogger wraps a *zap.Logger to satisfy the Logger interface.
type zapLogger struct {
	logger *zap.Logger
}

// NewZapLogger creates a new zap-backed Logger.
func NewZapLogger(opts *LoggerOptions) (Logger, error) {
	encoderConfig := zap.NewProductionEncoderConfig()

	encoderConfig.EncodeName = zapcore.FullNameEncoder
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.CallerKey = "caller"

	var encoder zapcore.Encoder
	switch strings.ToLower(opts.LogFormat) {
	case "text":
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	case "json":
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	default:
		return nil, fmt.Errorf("invalid log format: must be 'json' or 'text'")
	}

	// Configure output
	var writeSyncer zapcore.WriteSyncer
	switch strings.ToLower(opts.LogOutput) {
	case "console":
		writeSyncer = zapcore.AddSync(os.Stdout)
	case "file":
		fileLogger, err := NewFileLogger(&FileLoggerOptions{
			LogDirectory:  opts.LogDirectory,
			LogFilePrefix: opts.LogFilePrefix,
			MaxSize:       opts.MaxFileSize,
			MaxAge:        opts.MaxAge,
		})
		if err != nil {
			return nil, err
		}
		writeSyncer = zapcore.AddSync(fileLogger)
	default:
		return nil, fmt.Errorf("invalid log output: must be 'console' or 'file'")
	}

	core := zapcore.NewCore(
		encoder,
		writeSyncer,
		parseLogLevel(opts.LogLevel),
	)

	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))

	return &zapLogger{logger: logger}, nil
}

func (z *zapLogger) Debug(msg string, fields ...Field) {
	z.logger.Debug(msg, toZapFields(fields)...)
}

func (z *zapLogger) Info(msg string, fields ...Field) {
	z.logger.Info(msg, toZapFields(fields)...)
}

func (z *zapLogger) Warn(msg string, fields ...Field) {
	z.logger.Warn(msg, toZapFields(fields)...)
}

func (z *zapLogger) Error(msg string, fields ...Field) {
	z.logger.Error(msg, toZapFields(fields)...)
}

func (z *zapLogger) Fatal(msg string, fields ...Field) {
	z.logger.Fatal(msg, toZapFields(fields)...)
}

func (z *zapLogger) Sync() error {
	return z.logger.Sync()
}

// toZapFields converts surv.Field values to zap.Field values.
func toZapFields(fields []Field) []zap.Field {
	zapFields := make([]zap.Field, len(fields))
	for i, f := range fields {
		zapFields[i] = zap.Any(f.Key, f.Value)
	}
	return zapFields
}

func parseLogLevel(level string) zap.AtomicLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case "INFO":
		return zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "WARN":
		return zap.NewAtomicLevelAt(zapcore.WarnLevel)
	case "ERROR":
		return zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	case "FATAL":
		return zap.NewAtomicLevelAt(zapcore.FatalLevel)
	default:
		return zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}
}
