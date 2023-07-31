import boto3
import cfnresponse
import json
import logging
from time import sleep

logger = logging.getLogger(__name__)

ASSUME_ROLE_POLICY = """{
"Version": "2012-10-17",
"Statement": [
    {
    "Effect": "Allow",
    "Principal": {
        "Service": "lambda.amazonaws.com"
    },
    "Action": "sts:AssumeRole"
    }
]
}"""


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    logger.debug(json.dumps(event))

    status = cfnresponse.SUCCESS
    physical_resource_id = event.get("PhysicalResourceId", context.log_stream_name)

    try:
        if event["RequestType"] == "Create":
            iam = boto3.client("iam")
            partition = event["ResourceProperties"]["Partition"]

            try:
                iam.create_role(
                    RoleName="CloudFormation-Kubernetes-VPC",
                    AssumeRolePolicyDocument=ASSUME_ROLE_POLICY,
                )
            except iam.exceptions.EntityAlreadyExistsException as e:
                logger.warning(e)

            while True:
                try:
                    iam.attach_role_policy(
                        RoleName="CloudFormation-Kubernetes-VPC",
                        PolicyArn=f"arn:{partition}:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole",
                    )
                    iam.attach_role_policy(
                        RoleName="CloudFormation-Kubernetes-VPC",
                        PolicyArn=f"arn:{partition}:iam::aws:policy/service-role/AWSLambdaENIManagementAccess",
                    )
                    break
                except iam.exceptions.NoSuchEntityException as e:
                    logger.warning(e)
                    sleep(30)
    except Exception:
        logger.exception("Unhandled exception")
        status = cfnresponse.FAILED
    finally:
        cfnresponse.send(
            event,
            context,
            status,
            {},
            physical_resource_id,
        )
