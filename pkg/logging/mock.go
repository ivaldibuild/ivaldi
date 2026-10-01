package logging

import "github.com/stretchr/testify/mock"

// MockLogger implements the Logger interface for testing.
type MockLogger struct {
	mock.Mock
}

// NewMockLogger creates a new mock logger.
func NewMockLogger() *MockLogger {
	return &MockLogger{}
}

func (m *MockLogger) Debug(msg string, fields ...Field) {
	m.Called(append([]any{msg}, fieldsToAny(fields)...)...)
}

func (m *MockLogger) Info(msg string, fields ...Field) {
	m.Called(append([]any{msg}, fieldsToAny(fields)...)...)
}

func (m *MockLogger) Warn(msg string, fields ...Field) {
	m.Called(append([]any{msg}, fieldsToAny(fields)...)...)
}

func (m *MockLogger) Error(msg string, fields ...Field) {
	m.Called(append([]any{msg}, fieldsToAny(fields)...)...)
}

func (m *MockLogger) Fatal(msg string, fields ...Field) {
	m.Called(append([]any{msg}, fieldsToAny(fields)...)...)
}

func (m *MockLogger) Sync() error {
	args := m.Called()
	return args.Error(0)
}

func fieldsToAny(fields []Field) []any {
	result := make([]any, len(fields))
	for i, f := range fields {
		result[i] = f
	}
	return result
}
