package resource

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewError(t *testing.T) {
	expected := &Error{
		code:    ErrCodeInvalidException,
		message: "Test Error",
	}
	e := NewError(ErrCodeInvalidException, "Test Error")
	assert.EqualValues(t, expected, e)
}

func TestErrorCode(t *testing.T) {
	expectedHCode := ErrCodeInvalidException
	e := NewError(ErrCodeInvalidException, "Test Error")
	assert.EqualValues(t, expectedHCode, e.Code())
}

func TestErrorMessage(t *testing.T) {
	expectedMsg := "Test Error"
	e := NewError(ErrCodeInvalidException, "Test Error")
	assert.EqualValues(t, expectedMsg, e.Message())
}
