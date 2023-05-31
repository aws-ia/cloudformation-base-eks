import boto3
import logging

# Provided through CrhelperLayer in amazon-eks-per-region-resources.template.yaml
from crhelper import CfnResource
import random
import string

logger = logging.getLogger(__name__)
helper = CfnResource(json_logging=True, log_level="DEBUG")
eks = boto3.client("eks")


def stabilize(pid, cluster_name):
    while True:
        try:
            status = eks.describe_fargate_profile(
                clusterName=cluster_name, fargateProfileName=pid
            )["fargateProfile"]["status"]
        except eks.exceptions.ResourceNotFoundException:
            return "DELETED"

        if status not in ["CREATING", "DELETING"]:
            return status


@helper.create
@helper.update
def create(event, _):
    pid = "{}-{}".format(
        event["LogicalResourceId"],
        "".join(random.choice(string.ascii_lowercase) for i in range(8)),  # nosec B311
    )
    kwargs = {
        "fargateProfileName": pid,
        "clusterName": event["ResourceProperties"]["ClusterName"],
        "podExecutionRoleArn": event["ResourceProperties"]["IamRole"],
        "subnets": event["ResourceProperties"]["Subnets"],
        "selectors": [],
    }
    labels = {
        s.split("=")[0]: s.split("=")[1]
        for s in event["ResourceProperties"].get("Labels", [])
    }

    for ns in event["ResourceProperties"]["Namespaces"]:
        selector = {"namespace": ns}

        if labels:
            selector["labels"] = labels

        kwargs["selectors"].append(selector)

    eks.create_fargate_profile(**kwargs)

    status = stabilize(pid, event["ResourceProperties"]["ClusterName"])
    if status != "ACTIVE":
        raise Exception(f"Fargate profile {pid} status is {status}")

    return pid


@helper.delete
def delete(event, _):
    # name > 100 cannot be valid, create must have failed before creation completed
    if len(event["PhysicalResourceId"]) >= 100:
        return
    try:
        eks.delete_fargate_profile(
            clusterName=event["ResourceProperties"]["ClusterName"],
            fargateProfileName=event["PhysicalResourceId"],
        )
    except eks.exceptions.ResourceNotFoundException:
        return

    status = stabilize(
        event["PhysicalResourceId"], event["ResourceProperties"]["ClusterName"]
    )

    if status != "DELETED":
        raise Exception(
            f"Fargate profile {event['PhysicalResourceId']} status is {status}"
        )


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    helper(event, context)
