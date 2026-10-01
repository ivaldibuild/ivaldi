package logging

// Field represents a key-value pair for structured logging.
type Field struct {
	Key   string
	Value any
}

// F creates a new log field.
func F(key string, value any) Field {
	return Field{Key: key, Value: value}
}

// Logger defines the interface for structured logging.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	Sync() error
}
