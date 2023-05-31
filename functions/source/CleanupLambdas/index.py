import boto3
import json
import logging

# Provided through CrhelperLayer in amazon-eks-per-region-resources.template.yaml
from crhelper import CfnResource

logger = logging.getLogger(__name__)
helper = CfnResource(json_logging=True, log_level="DEBUG")
lambda_client = boto3.client("lambda")


@helper.delete
def delete_handler(event, _):
    security_group_id = event["ResourceProperties"]["SecurityGroupId"]
    paginator = lambda_client.get_paginator("list_functions")

    for page in paginator.paginate():
        for function in page["Functions"]:
            vpc_config = function.get("VpcConfig", {})
            security_group_ids = vpc_config.get("SecurityGroupIds", [])

            if security_group_id in security_group_ids:
                logger.info(f"deleting {function['FunctionName']}")

                lambda_client.delete_function(FunctionName=function["FunctionName"])


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    logger.debug(json.dumps(event))

    helper(event, context)
