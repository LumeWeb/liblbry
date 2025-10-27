package protocol

import (
	"bytes"
	"encoding/json"
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

	// Test structured logging
	logrusLogger.WithFields(logrus.Fields{
		"key1": "value1",
		"key2": 42,
	}).Info("test message")

	// Parse the JSON output
	output := buffer.String()
	var logEntry map[string]interface{}
	err := json.Unmarshal([]byte(output), &logEntry)
	assert.NoError(t, err, "Failed to parse log output as JSON")

	// Verify structured fields
	assert.Equal(t, "test message", logEntry["msg"])
	assert.Equal(t, "value1", logEntry["key1"])
	assert.Equal(t, float64(42), logEntry["key2"]) // JSON numbers are float64
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
