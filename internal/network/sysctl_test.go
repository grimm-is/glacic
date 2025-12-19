package network

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadSysctl(t *testing.T) {
	mockSys := new(MockSystemController)
	originalController := DefaultSystemController
	DefaultSystemController = mockSys
	defer func() { DefaultSystemController = originalController }()

	// Success
	mockSys.On("ReadSysctl", "/proc/sys/net/ipv4/ip_forward").Return("1", nil).Once()
	val, err := ReadSysctl("/proc/sys/net/ipv4/ip_forward")
	assert.NoError(t, err)
	assert.Equal(t, "1", val)

	// Failure
	mockSys.On("ReadSysctl", "/invalid/path").Return("", errors.New("read error")).Once()
	val, err = ReadSysctl("/invalid/path")
	assert.Error(t, err)
	assert.Empty(t, val)

	mockSys.AssertExpectations(t)
}

func TestWriteSysctl(t *testing.T) {
	mockSys := new(MockSystemController)
	originalController := DefaultSystemController
	DefaultSystemController = mockSys
	defer func() { DefaultSystemController = originalController }()

	// Success
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv4/ip_forward", "1").Return(nil).Once()
	err := WriteSysctl("/proc/sys/net/ipv4/ip_forward", "1")
	assert.NoError(t, err)

	// Failure
	mockSys.On("WriteSysctl", "/proc/sys/net/ipv4/ip_forward", "invalid").Return(errors.New("write error")).Once()
	err = WriteSysctl("/proc/sys/net/ipv4/ip_forward", "invalid")
	assert.Error(t, err)

	mockSys.AssertExpectations(t)
}

func TestIsNotExist(t *testing.T) {
	mockSys := new(MockSystemController)
	originalController := DefaultSystemController
	DefaultSystemController = mockSys
	defer func() { DefaultSystemController = originalController }()

	notExistErr := errors.New("file does not exist")

	mockSys.On("IsNotExist", notExistErr).Return(true).Once()
	assert.True(t, IsNotExist(notExistErr))

	otherErr := errors.New("other error")
	mockSys.On("IsNotExist", otherErr).Return(false).Once()
	assert.False(t, IsNotExist(otherErr))
}
