import boto3
import cfnresponse
import json
import logging
from botocore.config import Config
from random import randint
from time import sleep
from uuid import uuid4

logger = logging.getLogger(__name__)

CONFIG = Config(retries={"max_attempts": 10, "mode": "standard"})


def waiter(cfn_client, operation, stack_id):
    logger.info(f"waiter({operation}, {stack_id}) started")
    retries = 50

    while True:
        retries -= 1
        response = cfn_client.describe_stacks(StackName=stack_id)
        stack = response["Stacks"][0]
        status = stack["StackStatus"]
        if status in ["CREATE_COMPLETE", "UPDATE_COMPLETE"]:
            break

        if (
            status.endswith("FAILED")
            or status in ["DELETE_COMPLETE", "UPDATE_ROLLBACK_COMPLETE"]
            or retries == 0
        ):
            raise RuntimeError(
                f"Stack operation failed: {operation} {status} {stack_id}"
            )

        sleep(randint(1000, 1500) / 100)  # nosec B311

    logger.info(f"waiter({operation}, {stack_id}) done")


def get_stacks(key, value, region=None):
    cfn_client = boto3.client("cloudformation", region_name=region, config=CONFIG)
    stacks = []

    for page in cfn_client.get_paginator("describe_stacks").paginate():
        stacks += page["Stacks"]
    stack = [stack for stack in stacks if {"Key": key, "Value": value} in stack["Tags"]]

    if not len(stack):
        return None

    stack_id = stack[0]["StackId"]
    status = stack[0]["StackStatus"]

    if status.endswith("_IN_PROGRESS"):
        operation = status.split("_")[0].lower()

        waiter(cfn_client, operation, stack_id)

        if operation == "delete":
            return None

    return stack_id


def put_stack(name, region, template_url, parameters, key):
    logger.info(f"put_stack({name}, {region}, {template_url}, {parameters}, {key})")

    # jitter to reduce the chance of concurrent queries racing
    sleep(randint(0, 6000) / 100)  # nosec B311

    if name == "AccountSharedResources":
        for r in [
            r["RegionName"] for r in boto3.client("ec2").describe_regions()["Regions"]
        ]:
            acc_stack = get_stacks(key, name, r)
            if acc_stack:
                region = r
                break

    stack_id = get_stacks(key, name, region)
    client = boto3.client("cloudformation", region_name=region)

    args = {
        "StackName": stack_id if stack_id else f"{key}-{name}",
        "TemplateURL": template_url,
        "Parameters": [
            {"ParameterKey": k, "ParameterValue": v} for k, v in parameters.items()
        ],
        "Capabilities": [
            "CAPABILITY_IAM",
            "CAPABILITY_NAMED_IAM",
            "CAPABILITY_AUTO_EXPAND",
        ],
        "OnFailure": "DELETE",
        "Tags": [{"Key": key, "Value": name}],
    }

    method = client.create_stack

    wait = "create"
    if stack_id:
        method = client.update_stack
        wait = "update"
        del args["OnFailure"]

    try:
        stack_id = method(**args)["StackId"]
    except Exception as e:
        if "No updates are to be performed" in str(e):
            return

        logger.exception("Error getting stack ID")
        raise

    waiter(client, wait, stack_id)


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    logger.debug(json.dumps(event))

    status = cfnresponse.SUCCESS
    physical_resource_id = event.get("PhysicalResourceId", context.log_stream_name)
    key = props["Key"]
    acc_uri = props["AccountTemplateUri"]
    bucket = acc_uri.split("https://")[1].split(".")[0]
    prefix = "/".join(acc_uri.split("/")[3:-2]) + "/"

    try:
        if event["RequestType"] != "Delete":
            retries = 10
            while True:
                retries -= 1

                try:
                    put_stack("AccountSharedResources", None, acc_uri, {}, key)
                    put_stack(
                        "RegionalSharedResources",
                        None,
                        props["RegionalTemplateUri"],
                        {
                            "QSS3BucketName": bucket,
                            "QSS3KeyPrefix": prefix,
                            "RandomStr": uuid4().hex,
                        },
                        key,
                    )
                    break
                except Exception:
                    logger.exception("Error executing put_stack")

                    if retries > 0:
                        sleep(randint(0, 3000) / 100)  # nosec B311
                    else:
                        raise
    except Exception:
        status = cfnresponse.FAILED
        logger.exception("Error processing request")
    finally:
        cfnresponse.send(event, context, status, {}, physical_resource_id)
