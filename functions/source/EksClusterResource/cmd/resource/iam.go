package resource

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/aws/aws-sdk-go/service/sts/stsiface"
	"strings"
)

func getCaller(svc stsiface.STSAPI) (*string, error) {
	response, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, err
	}
	return toRoleArn(response.Arn), nil
}

func accountIdFromArn(arn *string) *string {
	accId := strings.Split(*arn, ":")[4]
	return &accId
}

func partitionFromArn(arn *string) *string {
	partition := strings.Split(*arn, ":")[1]
	return &partition
}

func isUserArn(arn *string) bool {
	return strings.Contains(*arn, ":user/")
}

func toRoleArn(arn *string) *string {
	arnParts := strings.Split(*arn, ":")
	if arnParts[2] != "sts" || !strings.HasPrefix(arnParts[5], "assumed-role") {
		return arn
	}
	arnParts = strings.Split(*arn, "/")
	arnParts[0] = strings.Replace(arnParts[0], "assumed-role", "role", 1)
	arnParts[0] = strings.Replace(arnParts[0], ":sts:", ":iam:", 1)
	arn = aws.String(arnParts[0] + "/" + arnParts[1])
	return arn
}
