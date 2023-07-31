package resource

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
)

const callbackDelaySeconds = 30

var LastKnownErrors []string

func errorEvent(model *Model, err *Error) handler.ProgressEvent {
	log.Printf("Returning ERROR...")
	log.Printf("HandlerErrorCode: %s, Message: %s", err.Code(), err.Message())
	return handler.ProgressEvent{
		OperationStatus:  handler.Failed,
		HandlerErrorCode: err.Code(),
		Message:          err.Message(),
		ResourceModel:    model,
	}
}

func successEvent(model *Model) handler.ProgressEvent {
	log.Printf("Returning SUCCESS...")
	return handler.ProgressEvent{
		OperationStatus: handler.Success,
		ResourceModel:   model,
	}
}

func inProgressEvent(model *Model, stage Stage) handler.ProgressEvent {
	log.Printf("Returning IN_PROGRESS next stage %v...\n", stage)
	return handler.ProgressEvent{
		OperationStatus: handler.InProgress,
		Message:         fmt.Sprintf("%v in progress\n", stage),
		ResourceModel:   model,
		CallbackContext: map[string]interface{}{
			"Stage":     stage,
			"StartTime": os.Getenv("StartTime"),
			"Name":      aws.StringValue(model.Name),
		},
		CallbackDelaySeconds: callbackDelaySeconds,
	}
}

func makeEvent(model *Model, nextStage Stage, err *Error) handler.ProgressEvent {
	if model != nil {
		timeout := checkTimeOut(os.Getenv("StartTime"), model.TimeOut)
		if timeout && nextStage != CompleteStage {
			errorString := fmt.Sprintf("resource creation timed out\n, LastKnownErrors: %s", strings.Join(LastKnownErrors, "\n "))
			return errorEvent(nil, NewError(ErrCodeTimeOut, errorString))
		}
	}
	if err != nil {
		return errorEvent(model, err)
	}
	if nextStage == CompleteStage {
		return successEvent(model)
	}
	return inProgressEvent(model, nextStage)
}
