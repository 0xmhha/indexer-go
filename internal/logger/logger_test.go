package logger

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestNewDevelopment tests development logger creation
func TestNewDevelopment(t *testing.T) {
	logger, err := NewDevelopment()
	if err != nil {
		t.Fatalf("NewDevelopment() error = %v", err)
	}

	if logger == nil {
		t.Fatal("NewDevelopment() returned nil logger")
	}

	// Logger should be usable
	logger.Info("test message")

	// Sync should not error
	if err := logger.Sync(); err != nil {
		// Ignore stdout sync errors on some platforms
		if !strings.Contains(err.Error(), "sync") {
			t.Errorf("Sync() error = %v", err)
		}
	}
}

// TestNewProduction tests production logger creation
func TestNewProduction(t *testing.T) {
	logger, err := NewProduction()
	if err != nil {
		t.Fatalf("NewProduction() error = %v", err)
	}

	if logger == nil {
		t.Fatal("NewProduction() returned nil logger")
	}

	// Logger should be usable
	logger.Info("test message")

	// Sync should not error
	if err := logger.Sync(); err != nil {
		// Ignore stdout sync errors on some platforms
		if !strings.Contains(err.Error(), "sync") {
			t.Errorf("Sync() error = %v", err)
		}
	}
}

// TestNewWithConfig tests logger creation with custom config
func TestNewWithConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid development config",
			config: &Config{
				Level:       "debug",
				Development: true,
				Encoding:    "console",
			},
			wantErr: false,
		},
		{
			name: "valid production config",
			config: &Config{
				Level:       "info",
				Development: false,
				Encoding:    "json",
			},
			wantErr: false,
		},
		{
			name: "invalid log level",
			config: &Config{
				Level:    "invalid",
				Encoding: "json",
			},
			wantErr: true,
		},
		{
			name: "empty encoding defaults to json",
			config: &Config{
				Level:    "info",
				Encoding: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := NewWithConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewWithConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if logger == nil {
					t.Error("NewWithConfig() returned nil logger")
					return
				}
				_ = logger.Sync()
			}
		})
	}
}

// TestLogLevels tests different log levels
func TestLogLevels(t *testing.T) {
	var buf bytes.Buffer

	// Create logger with custom output
	encoderCfg := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "time",
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(&buf),
		zapcore.DebugLevel,
	)

	logger := zap.New(core)

	tests := []struct {
		name     string
		logFunc  func(string, ...zap.Field)
		message  string
		wantLog  bool
		minLevel zapcore.Level
	}{
		{
			name:     "debug level",
			logFunc:  logger.Debug,
			message:  "debug message",
			wantLog:  true,
			minLevel: zapcore.DebugLevel,
		},
		{
			name:     "info level",
			logFunc:  logger.Info,
			message:  "info message",
			wantLog:  true,
			minLevel: zapcore.InfoLevel,
		},
		{
			name:     "warn level",
			logFunc:  logger.Warn,
			message:  "warn message",
			wantLog:  true,
			minLevel: zapcore.WarnLevel,
		},
		{
			name:     "error level",
			logFunc:  logger.Error,
			message:  "error message",
			wantLog:  true,
			minLevel: zapcore.ErrorLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc(tt.message)

			output := buf.String()
			if tt.wantLog && output == "" {
				t.Error("Expected log output but got none")
			}

			if tt.wantLog && !strings.Contains(output, tt.message) {
				t.Errorf("Log output doesn't contain message: %s", output)
			}
		})
	}
}

// TestStructuredLogging tests structured field logging
func TestStructuredLogging(t *testing.T) {
	var buf bytes.Buffer

	encoderCfg := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(&buf),
		zapcore.InfoLevel,
	)

	logger := zap.New(core)

	// Log with structured fields
	logger.Info("test message",
		zap.String("string_field", "value"),
		zap.Int("int_field", 42),
		zap.Bool("bool_field", true),
	)

	output := buf.String()

	// Check that structured fields are in the output
	expectedStrings := []string{
		"test message",
		"string_field",
		"value",
		"int_field",
		"42",
		"bool_field",
		"true",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Log output missing expected string %q: %s", expected, output)
		}
	}
}

// TestContextLogger tests context-aware logging
func TestContextLogger(t *testing.T) {
	logger, err := NewDevelopment()
	if err != nil {
		t.Fatalf("NewDevelopment() error = %v", err)
	}

	ctx := context.Background()

	// Add logger to context
	ctx = WithLogger(ctx, logger)

	// Retrieve logger from context
	retrievedLogger := FromContext(ctx)
	if retrievedLogger == nil {
		t.Fatal("FromContext() returned nil logger")
	}

	// Should be able to use the retrieved logger
	retrievedLogger.Info("test from context")

	// Test with empty context (no logger set) - should return fallback
	emptyCtxLogger := FromContext(context.TODO())
	if emptyCtxLogger == nil {
		t.Error("FromContext with empty context should return fallback logger, not nil")
	}
}

// TestContextLoggerFallback tests fallback when no logger in context
func TestContextLoggerFallback(t *testing.T) {
	ctx := context.Background()

	// Get logger from context without setting one
	logger := FromContext(ctx)
	if logger == nil {
		t.Fatal("FromContext() should return nop logger, not nil")
	}

	// Should not panic when used
	logger.Info("test message")
}

// TestLoggerWithFields tests adding fields to logger
func TestLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer

	encoderCfg := zapcore.EncoderConfig{
		MessageKey:  "msg",
		LevelKey:    "level",
		EncodeLevel: zapcore.LowercaseLevelEncoder,
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(&buf),
		zapcore.InfoLevel,
	)

	baseLogger := zap.New(core)

	// Create logger with preset fields
	logger := baseLogger.With(
		zap.String("component", "test"),
		zap.String("version", "1.0"),
	)

	logger.Info("test message")

	output := buf.String()

	// Check that preset fields are in the output
	if !strings.Contains(output, "component") {
		t.Error("Log output missing component field")
	}
	if !strings.Contains(output, "test") {
		t.Error("Log output missing component value")
	}
	if !strings.Contains(output, "version") {
		t.Error("Log output missing version field")
	}
}

// TestConfigValidation tests config validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "empty level defaults to info",
			config: &Config{
				Level:    "",
				Encoding: "json",
			},
			wantErr: false,
		},
		{
			name: "valid config",
			config: &Config{
				Level:       "debug",
				Development: true,
				Encoding:    "console",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWithConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewWithConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
