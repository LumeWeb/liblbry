package protocol

import (
	"bytes"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestZapToLogrusAdapter(t *testing.T) {
	// Create a buffer to capture log output
	var buffer bytes.Buffer

	// Create a zap logger that writes to our buffer
	zapCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(zapcore.EncoderConfig{
			TimeKey:    "time",
			LevelKey:   "level",
			NameKey:    "logger",
			CallerKey:  "caller",
			MessageKey: "msg",
		}),
		zapcore.AddSync(&buffer),
		zapcore.DebugLevel,
	)
	zapLogger := zap.New(zapCore)

	// Create our adapter
	logrusLogger := NewZapToLogrusAdapter(zapLogger)

	// Test logging
	logrusLogger.Info("test message", "key1", "value1", "key2", 42)

	// Check that something was written
	output := buffer.String()
	assert.Contains(t, output, "test message")
	assert.Contains(t, output, "value1")
	assert.Contains(t, output, "42")
}

func TestZapHook(t *testing.T) {
	// Create a buffer to capture log output
	var buffer bytes.Buffer

	// Create a zap logger that writes to our buffer
	zapCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(zapcore.EncoderConfig{
			TimeKey:    "time",
			LevelKey:   "level",
			NameKey:    "logger",
			CallerKey:  "caller",
			MessageKey: "msg",
		}),
		zapcore.AddSync(&buffer),
		zapcore.DebugLevel,
	)
	zapLogger := zap.New(zapCore)

	// Create hook
	hook := &zapHook{zapLogger: zapLogger}

	// Test different log levels
	testCases := []struct {
		level    string
		logFunc  func()
		expected string
	}{
		{
			level: "debug",
			logFunc: func() {
				hook.Fire(&logrus.Entry{
					Level:   logrus.DebugLevel,
					Message: "debug message",
					Data:    map[string]interface{}{"key": "debug_value"},
				})
			},
			expected: "debug message",
		},
		{
			level: "info",
			logFunc: func() {
				hook.Fire(&logrus.Entry{
					Level:   logrus.InfoLevel,
					Message: "info message",
					Data:    map[string]interface{}{"key": "info_value"},
				})
			},
			expected: "info message",
		},
		{
			level: "warn",
			logFunc: func() {
				hook.Fire(&logrus.Entry{
					Level:   logrus.WarnLevel,
					Message: "warn message",
					Data:    map[string]interface{}{"key": "warn_value"},
				})
			},
			expected: "warn message",
		},
		{
			level: "error",
			logFunc: func() {
				hook.Fire(&logrus.Entry{
					Level:   logrus.ErrorLevel,
					Message: "error message",
					Data:    map[string]interface{}{"key": "error_value"},
				})
			},
			expected: "error message",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.level, func(t *testing.T) {
			buffer.Reset()
			tc.logFunc()
			output := buffer.String()
			assert.Contains(t, output, tc.expected)
			assert.Contains(t, output, tc.level+"_value")
		})
	}
}
