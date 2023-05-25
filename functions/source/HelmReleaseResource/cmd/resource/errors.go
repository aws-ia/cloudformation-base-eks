package resource

const (
	// ErrCodeHelmActionException The specified helm action errors
	ErrCodeHelmActionException = "HelmActionException"

	// ErrCodeKubeException The specified helm action errors
	ErrCodeKubeException = "KubeException"

	// ErrCodeLambdaException The specified lambda errors
	ErrCodeLambdaException = "LambdaException"

	ErrCodeInvalidException = "InvalidRequest"

	ErrCodeNotFound = "NotFound"

	ErrCodeTimeOut = "TimeOut"
)

type Error struct {
	// Classification of error
	code string

	// Detailed information about error
	message string
}

func NewError(code, message string) *Error {
	b := &Error{
		code:    code,
		message: message,
	}
	return b
}

func (b Error) Code() string {
	return b.code
}

// Message returns the error details message.
func (b Error) Message() string {
	return b.message
}
