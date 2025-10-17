package logger

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config holds logger configuration
type Config struct {
	// Level is the minimum enabled logging level
	// Valid values: "debug", "info", "warn", "error", "dpanic", "panic", "fatal"
	// Default: "info"
	Level string

	// Development enables development mode (human-readable output, stack traces)
	Development bool

	// Encoding sets the logger's encoding
	// Valid values: "json", "console"
	// Default: "json"
	Encoding string

	// OutputPaths is a list of URLs or file paths to write logging output to
	// Default: ["stdout"]
	OutputPaths []string

	// ErrorOutputPaths is a list of URLs or file paths to write error output to
	// Default: ["stderr"]
	ErrorOutputPaths []string

	// InitialFields is a collection of fields to add to the root logger
	InitialFields map[string]interface{}
}

// contextKey is a private type for context keys to avoid collisions
type contextKey struct{}

// loggerKey is the context key for storing logger instances
var loggerKey = contextKey{}

// NewDevelopment creates a development logger with reasonable defaults
// - Debug level enabled
// - Console encoding (human-readable)
// - Stack traces for warnings and above
// - Development mode enabled
func NewDevelopment() (*zap.Logger, error) {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	return config.Build()
}

// NewProduction creates a production logger with reasonable defaults
// - Info level enabled
// - JSON encoding
// - Sampling enabled
// - Stack traces for errors and above
func NewProduction() (*zap.Logger, error) {
	config := zap.NewProductionConfig()
	return config.Build()
}

// NewWithConfig creates a logger with the specified configuration
func NewWithConfig(cfg *Config) (*zap.Logger, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Set defaults
	if cfg.Level == "" {
		cfg.Level = "info"
	}
	if cfg.Encoding == "" {
		cfg.Encoding = "json"
	}
	if len(cfg.OutputPaths) == 0 {
		cfg.OutputPaths = []string{"stdout"}
	}
	if len(cfg.ErrorOutputPaths) == 0 {
		cfg.ErrorOutputPaths = []string{"stderr"}
	}

	// Parse log level
	level := zap.NewAtomicLevel()
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
	}

	// Build encoder config
	var encoderConfig zapcore.EncoderConfig
	if cfg.Development {
		encoderConfig = zap.NewDevelopmentEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		encoderConfig = zap.NewProductionEncoderConfig()
	}

	// Create zap config
	zapConfig := zap.Config{
		Level:             level,
		Development:       cfg.Development,
		Encoding:          cfg.Encoding,
		EncoderConfig:     encoderConfig,
		OutputPaths:       cfg.OutputPaths,
		ErrorOutputPaths:  cfg.ErrorOutputPaths,
		InitialFields:     cfg.InitialFields,
		DisableCaller:     false,
		DisableStacktrace: !cfg.Development,
	}

	// Build logger
	logger, err := zapConfig.Build(
		zap.AddCallerSkip(0),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	return logger, nil
}

// WithLogger returns a new context with the given logger attached
func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext retrieves the logger from the context
// If no logger is found, it returns a no-op logger
func FromContext(ctx context.Context) *zap.Logger {
	if ctx == nil {
		return zap.NewNop()
	}

	if logger, ok := ctx.Value(loggerKey).(*zap.Logger); ok && logger != nil {
		return logger
	}

	return zap.NewNop()
}

// WithComponent returns a logger with a "component" field
func WithComponent(logger *zap.Logger, component string) *zap.Logger {
	return logger.With(zap.String("component", component))
}

// WithFields returns a logger with additional fields
func WithFields(logger *zap.Logger, fields ...zap.Field) *zap.Logger {
	return logger.With(fields...)
}
