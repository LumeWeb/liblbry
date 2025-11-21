package protocol

import (
	"io"

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
	logrusLogger := logrus.New()
	if zapLogger == nil {
		// Default to discarding output if no zap provided
		logrusLogger.SetOutput(io.Discard)
		return logrusLogger
	}
	// Send everything through the zap hook; disable logrus formatter output.
	logrusLogger.SetOutput(io.Discard)
	logrusLogger.AddHook(&zapHook{zapLogger: zapLogger})
	logrusLogger.SetLevel(mapZapMinLevelToLogrus(zapLogger))
	return logrusLogger
}

// zapHook forwards logrus entries to zap with proper levels and structured fields
type zapHook struct {
	zapLogger *zap.Logger
}

func (h *zapHook) Levels() []logrus.Level { return logrus.AllLevels }

func (h *zapHook) Fire(e *logrus.Entry) error {
	if h.zapLogger == nil {
		return nil
	}
	// transfer structured fields
	fields := make([]zap.Field, 0, len(e.Data))
	for k, v := range e.Data {
		fields = append(fields, zap.Any(k, v))
	}
	l := h.zapLogger.With(fields...)
	switch e.Level {
	case logrus.PanicLevel, logrus.FatalLevel, logrus.ErrorLevel:
		l.Error(e.Message)
	case logrus.WarnLevel:
		l.Warn(e.Message)
	case logrus.InfoLevel:
		l.Info(e.Message)
	case logrus.TraceLevel, logrus.DebugLevel:
		l.Debug(e.Message)
	default:
		l.Info(e.Message)
	}
	return nil
}

// mapZapMinLevelToLogrus inspects zap's enabled levels to derive a logrus Level.
func mapZapMinLevelToLogrus(z *zap.Logger) logrus.Level {
	if z == nil {
		return logrus.InfoLevel
	}
	levels := []zapcore.Level{
		zapcore.DebugLevel, zapcore.InfoLevel, zapcore.WarnLevel,
		zapcore.ErrorLevel, zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel,
	}
	min := zapcore.InfoLevel
	for _, lv := range levels {
		if z.Core().Enabled(lv) {
			min = lv
			break
		}
	}
	switch min {
	case zapcore.DebugLevel:
		return logrus.DebugLevel
	case zapcore.InfoLevel:
		return logrus.InfoLevel
	case zapcore.WarnLevel:
		return logrus.WarnLevel
	default:
		return logrus.ErrorLevel
	}
}
