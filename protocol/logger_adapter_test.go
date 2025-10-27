package protocol

import (
	"bytes"
	"testing"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestZapToLogrusAdapter(t *testing.T) {
	// Create a buffer to capture log output
	var buffer bytes.Buffer
	
	// Create a zap logger that writes to our buffer
	zapCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(zapcore.EncoderConfig{
			TimeKey:        "time",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
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


func TestZapWriter(t *testing.T) {
	// Create a buffer to capture output
	var buffer bytes.Buffer
	
	// Create a zap logger
	zapCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(zapcore.EncoderConfig{
			TimeKey:        "time",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
		}),
		zapcore.AddSync(&buffer),
		zapcore.DebugLevel,
	)
	zapLogger := zap.New(zapCore)
	
	// Create writer
	writer := &zapWriter{zapLogger: zapLogger}
	
	// Write some data
	n, err := writer.Write([]byte("test message"))
	require.NoError(t, err)
	assert.Equal(t, 12, n)
	
	// Check output contains our message
	output := buffer.String()
	assert.Contains(t, output, "test message")
}
