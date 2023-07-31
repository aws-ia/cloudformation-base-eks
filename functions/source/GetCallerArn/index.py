import logging
import json
from datetime import timedelta
from time import sleep
import boto3

# Provided through CrhelperLayer in amazon-eks-per-region-resources.template.yaml
from crhelper import CfnResource
import traceback

logger = logging.getLogger(__name__)
helper = CfnResource(json_logging=True, log_level="DEBUG")

try:
    cfn_client = boto3.client("cloudformation")
    ct_client = boto3.client("cloudtrail")
except Exception as init_exception:
    helper.init_failure(init_exception)


def get_caller_arn(stack_id):
    try:
        root_id = cfn_client.describe_stacks(StackName=stack_id)["Stacks"][0]["RootId"]
    except ValueError:
        traceback.print_exc()
        return "NotFound"
    except IndexError:
        traceback.print_exc()
        return "NotFound"

    create_time = cfn_client.describe_stacks(StackName=root_id)["Stacks"][0][
        "CreationTime"
    ]

    retries = 50
    while True:
        retries -= 1
        try:
            response = ct_client.lookup_events(
                LookupAttributes=[
                    {"AttributeKey": "ResourceName", "AttributeValue": root_id},
                    {"AttributeKey": "EventName", "AttributeValue": "CreateStack"},
                ],
                StartTime=create_time - timedelta(minutes=15),
                EndTime=create_time + timedelta(minutes=15),
            )

            if len(response["Events"]) > 0:
                return sts_to_role(
                    json.loads(response["Events"][0]["CloudTrailEvent"])[
                        "userIdentity"
                    ]["arn"]
                )

            logger.info("Event not in cloudtrail yet, %s retries left" % str(retries))
        except Exception:
            logger.exception("Unhandled exception")

        if retries == 0:
            logger.warning("Ran out of retries!")
            return "NotFound"

        sleep(15)


def sts_to_role(sts_arn):
    logger.debug(f"arn from cloudtrail: {sts_arn}")

    if ":sts::" not in sts_arn or not sts_arn.split("/")[0].endswith("assumed-role"):
        return sts_arn

    if len(sts_arn.split("/")) < 2:
        logger.error(f"failed to parse calling arn {sts_arn}")
        return "NotFound"

    role_name = sts_arn.split("/")[1]
    account_id = sts_arn.split(":")[4]

    return f"arn:aws:iam::{account_id}:role/{role_name}"


@helper.create
def create(event, _):
    try:
        arn = get_caller_arn(event["StackId"])
        helper.Data["Arn"] = arn

        if len(arn.split("/")) < 2:
            return arn

        return arn.split("/")[1]
    except Exception:
        logger.exception("Unexpected error")
        helper.Data["Arn"] = "NotFound"

        return "NotFound"


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    helper(event, context)
