package protocol

import (
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapToLogrusAdapter adapts a zap logger to implement the logrus logger interface
// This allows the DHT (which expects logrus.Logger) to use a zap logger internally
type ZapToLogrusAdapter struct {
	zapLogger *zap.Logger
}

// NewZapToLogrusAdapter creates a new adapter that wraps a zap logger
func NewZapToLogrusAdapter(zapLogger *zap.Logger) *logrus.Logger {
	// Create a logrus logger that will delegate to the zap logger
	logrusLogger := logrus.New()

	// Replace the output to use our custom writer
	logrusLogger.SetOutput(&zapWriter{zapLogger: zapLogger})

	// Set the level to match zap logger's level
	// Note: This is a basic approximation - zap has more granular levels
	logrusLogger.SetLevel(convertZapToLogrusLevel(zapLogger.Core().Enabled(zapcore.DebugLevel)))

	return logrusLogger
}

// zapWriter implements io.Writer to redirect logrus output to zap logger
type zapWriter struct {
	zapLogger *zap.Logger
}

// Write implements io.Writer interface
func (w *zapWriter) Write(p []byte) (n int, err error) {
	// Parse the logrus output and redirect to zap
	// This is a simplified approach - in practice, logrus formats messages
	// For now, we'll treat the entire byte slice as a message
	message := string(p)

	// Log at info level (logrus doesn't expose the level in Write)
	w.zapLogger.Info(message)

	return len(p), nil
}

// convertZapToLogrusLevel converts zap level to logrus level
func convertZapToLogrusLevel(enabled bool) logrus.Level {
	if enabled {
		return logrus.DebugLevel
	}
	return logrus.InfoLevel
}

