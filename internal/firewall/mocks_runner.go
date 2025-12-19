package firewall

import (
	"github.com/stretchr/testify/mock"
)

// MockCommandRunner is a mock implementation of CommandRunner for testing.
// Moved here from mocks.go to allow cross-platform testing.
type MockCommandRunner struct {
	mock.Mock
}

func (m *MockCommandRunner) Run(name string, args ...string) error {
	callArgs := make([]interface{}, 0, len(args)+1)
	callArgs = append(callArgs, name)
	for _, a := range args {
		callArgs = append(callArgs, a)
	}
	result := m.Called(callArgs...)
	return result.Error(0)
}

func (m *MockCommandRunner) Output(name string, args ...string) ([]byte, error) {
	callArgs := make([]interface{}, 0, len(args)+1)
	callArgs = append(callArgs, name)
	for _, a := range args {
		callArgs = append(callArgs, a)
	}
	result := m.Called(callArgs...)
	if result.Get(0) == nil {
		return nil, result.Error(1)
	}
	return result.Get(0).([]byte), result.Error(1)
}

func (m *MockCommandRunner) RunInput(input string, name string, args ...string) error {
	callArgs := make([]interface{}, 0, len(args)+2)
	callArgs = append(callArgs, input, name)
	for _, a := range args {
		callArgs = append(callArgs, a)
	}
	result := m.Called(callArgs...)
	return result.Error(0)
}
