package liblbry

import (
	"fmt"
	"github.com/knadh/koanf/v2"
	"go.uber.org/zap"
	"reflect"
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

	// Check if T is a pointer type to avoid nil pointer issues
	if reflect.ValueOf(factory).Kind() == reflect.Ptr {
		return factory, fmt.Errorf("T must not be a pointer type - use concrete type instead")
	}

	// Get addressable value for proper method resolution
	target := any(&factory)

	for _, opt := range opts {
		if err := opt.Apply(target); err != nil {
			return factory, err
		}
	}

	// Ensure predictable logger defaulting
	if lg, ok := target.(LoggerGetter); ok {
		if lg.GetLogger() == nil {
			if ls, ok := target.(LoggerSetter); ok {
				ls.SetLogger(defaultLogger)
			}
		}
	}

	return factory, nil
}
