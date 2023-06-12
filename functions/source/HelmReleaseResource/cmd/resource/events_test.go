package resource

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

func validateContext(t *testing.T, h handler.ProgressEvent, expectedContext map[string]interface{}) {
	t.Helper()
	assert.EqualValues(t, expectedContext, h.CallbackContext)
}

func validateOStatus(t *testing.T, h handler.ProgressEvent, expectedStatus handler.Status) {
	t.Helper()
	assert.EqualValues(t, expectedStatus, h.OperationStatus)
}

func validateMessage(t *testing.T, h handler.ProgressEvent, e string) {
	t.Helper()
	assert.EqualValues(t, e, h.Message)
}

func validateHCode(t *testing.T, h handler.ProgressEvent, e string) {
	t.Helper()
	assert.EqualValues(t, e, h.HandlerErrorCode)
}

func TestErrorEvent(t *testing.T) {
	expectedMessage := "Test Error"
	expectedStatus := handler.Failed
	expectedHCode := ErrCodeInvalidException
	m := &Model{
		Name: aws.String("Test"),
	}
	result := errorEvent(m, NewError(ErrCodeInvalidException, "Test Error"))
	validateMessage(t, result, expectedMessage)
	validateOStatus(t, result, expectedStatus)
	validateHCode(t, result, expectedHCode)
}

func TestSuccessEvent(t *testing.T) {
	expectedStatus := handler.Success
	m := &Model{
		Name: aws.String("Test"),
	}
	result := successEvent(m)
	validateOStatus(t, result, expectedStatus)
}

func TestInProgressEvent(t *testing.T) {
	os.Unsetenv("StartTime")
	defer os.Unsetenv("StartTime")
	//st := time.Now().Format(time.RFC3339)
	/* expectedContext := map[string]interface{}{
		"Stage":     LambdaInitStage,
		"StartTime": st,
		"Name":      "Test",
	} */
	expectedStatus := handler.InProgress
	m := &Model{
		Name: aws.String("Test"),
	}
	result := inProgressEvent(m, Stage("LambdaInit"))
	//validateContext(t, result, expectedContext)
	validateOStatus(t, result, expectedStatus)
}

func TestMakeEvent(t *testing.T) {
	os.Unsetenv("StartTime")
	defer os.Unsetenv("StartTime")
	st := time.Now().Format(time.RFC3339)
	tests := map[string]struct {
		m               *Model
		stage           Stage
		err             *Error
		expectedContext map[string]interface{}
		expectedStatus  handler.Status
		expectedMessage string
	}{
		"Inprogress": {
			m: &Model{
				Name: aws.String("Test"),
			},
			stage: ReleaseStabilize,
			err:   nil,
			expectedContext: map[string]interface{}{
				"Stage":     ReleaseStabilize,
				"StartTime": st,
				"Name":      "Test",
			},
			expectedStatus:  handler.InProgress,
			expectedMessage: fmt.Sprintf("%v in progress\n", ReleaseStabilize),
		},
		"Failure": {
			m: &Model{
				Name: aws.String("Test"),
			},
			stage:           ReleaseStabilize,
			err:             NewError(ErrCodeInvalidException, "Test Error"),
			expectedMessage: "Test Error",
			expectedStatus:  handler.Failed,
			expectedContext: nil,
		},
		"Success": {
			m: &Model{
				Name: aws.String("Test"),
			},
			stage:           CompleteStage,
			err:             nil,
			expectedMessage: "",
			expectedStatus:  handler.Success,
			expectedContext: nil,
		},
		"TimeOut": {
			m: &Model{
				Name: aws.String("Test"),
			},
			stage:           ReleaseStabilize,
			err:             nil,
			expectedMessage: "resource creation timed out\n, LastKnownErrors: Test",
			expectedStatus:  handler.Failed,
			expectedContext: nil,
		},
		"TimeOutWithCompleteStage": {
			m: &Model{
				Name: aws.String("Test"),
			},
			stage:           CompleteStage,
			err:             nil,
			expectedMessage: "",
			expectedStatus:  handler.Success,
			expectedContext: nil,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			switch name {
			case "TimeOut", "TimeOutWithCompleteStage":
				LastKnownErrors = []string{"Test"}
				os.Setenv("StartTime", time.Now().Add(time.Hour*-10).Format(time.RFC3339))
			default:
				os.Setenv("StartTime", st)
			}
			res := makeEvent(d.m, d.stage, d.err)
			validateOStatus(t, res, d.expectedStatus)
			validateMessage(t, res, d.expectedMessage)
			validateContext(t, res, d.expectedContext)
		})
	}
}
