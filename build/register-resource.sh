#!/bin/bash

TMP_BUCKET=false

if [ "${RESOURCE_PATH}" == "" ] ; then
  echo RESOURCE_PATH must be specified to publish
  exit 1
fi
if [ "${RESOURCE_TYPE}" == "" ] ; then
  echo RESOURCE_TYPE must be specified to publish
  exit 1
fi
if [ "${RESOURCE_FILE}" == "" ] ; then
  echo RESOURCE_FILE must be specified to publish
  exit 1
fi

EXIT=0
if [ "${BUCKET}" == "" ] ; then
    TMP_BUCKET="eksqs-tmp-$(LC_CTYPE=C tr -dc 'a-z0-9' </dev/urandom | fold -w 16 | head -n 1)"
    aws s3 mb s3://${TMP_BUCKET} --region ${REGION} --profile ${PROFILE}
    cat <<EOF > /tmp/policy.json
{
   "Statement": [
      {
         "Effect": "Allow",
         "Principal": {"Service": "cloudformation.amazonaws.com"},
         "Action": ["s3:GetObject", "s3:ListBucket"],
         "Resource": ["arn:aws:s3:::${TMP_BUCKET}", "arn:aws:s3:::${TMP_BUCKET}/*"]
      }
   ]
}
EOF
    aws s3api put-bucket-policy --bucket ${TMP_BUCKET} --policy file:///tmp/policy.json --region ${REGION} --profile ${PROFILE} || EXIT=$?
    BUCKET=${TMP_BUCKET}
fi

build/lambda_package.sh ${RESOURCE_PATH} || EXIT=$?
if [ $EXIT -eq 0 ]; then
  aws s3 cp functions/packages/${RESOURCE_PATH}/${RESOURCE_FILE} s3://${BUCKET} --region ${REGION} --profile ${PROFILE} || EXIT=$?
fi
if [ $EXIT -eq 0 ]; then
  cur_resource=$(aws cloudformation describe-type --type RESOURCE --type-name ${RESOURCE_TYPE} --region ${REGION} --profile ${PROFILE})
  log_role_arn=$(echo $cur_resource | jq -r .LoggingConfig.LogRoleArn)
  log_group_name=$(echo $cur_resource | jq -r .LoggingConfig.LogGroupName)
  role_arn=$(echo $cur_resource | jq -r .ExecutionRoleArn)
  token=$(aws cloudformation register-type --type "RESOURCE" --type-name ${RESOURCE_TYPE} --schema-handler-package s3://${BUCKET}/${RESOURCE_FILE} --logging-config LogRoleArn=${log_role_arn},LogGroupName=${log_group_name} --region ${REGION} --execution-role-arn ${role_arn} --profile ${PROFILE} --output text --query RegistrationToken || EXIT=$?)
fi

if [ $EXIT -eq 0 ] ; then
  while true; do
    resp=$(aws cloudformation describe-type-registration --registration-token $token  --region ${REGION} --profile ${PROFILE})
    DESC=$(echo $resp | jq -r .Description)
    if [ "$DESC" != "$LAST_DESC" ] ; then
      echo $DESC
    fi
    LAST_DESC=$DESC
    if [ "$(echo $resp | jq -r .ProgressStatus)" == "COMPLETE" ] ; then
      TYPE_VERSION=$(echo $resp | jq -r .TypeVersionArn)
      break
    fi
    if [ "$(echo $resp | jq -r .ProgressStatus)" != "IN_PROGRESS" ] ; then
      EXIT=1
      break
    fi
    sleep 15
  done
  aws cloudformation set-type-default-version --arn ${TYPE_VERSION}  --region ${REGION} --profile ${PROFILE} || EXIT=$?
fi
if $TMP_BUCKET ; then
  aws s3 rb s3://${TMP_BUCKET} --region ${REGION} --profile ${PROFILE} --force || EXIT=$?
fi
exit ${EXIT}
