package liblbry

import (
	"github.com/knadh/koanf/v2"
	"go.uber.org/zap"
)

// StoreFactory defines the interface for creating blob storage instances
type StoreFactory interface {
	CreateStore(config *koanf.Koanf) (BlobStore, error)
	Name() string
}

// LoggerGetter defines an interface for types that can provide their logger
type LoggerGetter interface {
	GetLogger() *zap.Logger
}

// LoggerSetter defines an interface for types that can have a logger configured
type LoggerSetter interface {
	SetLogger(*zap.Logger)
}

// defaultLogger is the package-level default logger
var defaultLogger = zap.NewNop()

// StoreFactoryOption defines a functional option for configuring StoreFactory implementations
type StoreFactoryOption interface {
	Apply(interface{}) error
}

// LoggerOption is a functional option for setting a logger
type LoggerOption struct {
	logger *zap.Logger
}

// Apply applies the logger option to a StoreFactory
func (l LoggerOption) Apply(factory interface{}) error {
	if f, ok := factory.(LoggerSetter); ok {
		f.SetLogger(l.logger)
	}
	return nil
}

// WithLogger creates a LoggerOption
func WithLogger(logger *zap.Logger) StoreFactoryOption {
	return LoggerOption{logger: logger}
}

// CreateStorageFactory is a generic helper function that creates instances of StoreFactory implementations
func CreateStorageFactory[T StoreFactory](logger *zap.Logger) (T, error) {
	return CreateStorageFactoryWithOptions[T](WithLogger(logger))
}

// CreateStorageFactoryWithOptions is a generic helper function that creates instances of StoreFactory implementations with options
func CreateStorageFactoryWithOptions[T StoreFactory](opts ...StoreFactoryOption) (T, error) {
	var factory T

	for _, opt := range opts {
		if err := opt.Apply(&factory); err != nil {
			return factory, err
		}
	}

	// Ensure predictable logger defaulting
	if lg, ok := any(&factory).(LoggerGetter); ok {
		if lg.GetLogger() == nil {
			if ls, ok := any(&factory).(LoggerSetter); ok {
				ls.SetLogger(defaultLogger)
			}
		}
	}

	return factory, nil
}
