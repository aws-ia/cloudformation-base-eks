import boto3
import os
import traceback
from string import ascii_lowercase
from random import choice
import json
import logging
import time
from typing import Optional, Union
from pathlib import Path
from cloudformation_cli_python_lib import SessionProxy, exceptions


LOG = logging.getLogger(__name__)
LOG.setLevel(logging.DEBUG)


def proxy_needed(
    cluster_name: str, boto3_session: Optional[Union[boto3.Session, SessionProxy]]
) -> (boto3.client, str):
    eks = boto3_session.client("eks")
    try:
        eks_vpc_config = eks.describe_cluster(name=cluster_name)["cluster"][
            "resourcesVpcConfig"
        ]
    except eks.exceptions.ResourceNotFoundException:
        raise exceptions.InvalidRequest(f"cluster with name {cluster_name} not found")
    # If there's no vpc zip then we're already in the inner lambda.
    if not Path("./awsqs_kubernetes_resource/vpc.zip").resolve().exists():
        return False
    # for now we will always use vpc proxy, until we can work out how to wrap boto3 session in CFN registry when authing
    if eks_vpc_config['endpointPublicAccess'] and '0.0.0.0/0' in eks_vpc_config['publicAccessCidrs']:
        LOG.warning("cluster is public")
        return False
    if this_invoke_is_inside_vpc(
        set(eks_vpc_config["subnetIds"]), set(eks_vpc_config["securityGroupIds"])
    ):
        return False
    return True


def this_invoke_is_inside_vpc(subnet_ids: set, sg_ids: set) -> bool:
    lmbd = boto3.client("lambda")
    try:
        lambda_config = lmbd.get_function_configuration(
            FunctionName=os.environ["AWS_LAMBDA_FUNCTION_NAME"]
        )
        l_vpc_id = lambda_config["VpcConfig"].get("VpcId", "")
        l_subnet_ids = set(lambda_config["VpcConfig"].get("subnetIds", ""))
        l_sg_ids = set(lambda_config["VpcConfig"].get("securityGroupIds", ""))
        if l_vpc_id and l_subnet_ids.issubset(subnet_ids) and l_sg_ids.issubset(sg_ids):
            return True
    except Exception:
        print(
            f'failed to get function config for {os.environ["AWS_LAMBDA_FUNCTION_NAME"]}'
        )
        traceback.print_exc()
    return False


def proxy_call(cluster_name, manifest, command, sess):
    event = {"cluster_name": cluster_name, "manifest": manifest, "command": command}
    resp = invoke_function(
        f"awsqs-kubernetes-resource-proxy-{cluster_name}", event, sess
    )
    if "errorMessage" in resp:
        LOG.error(f'Code: {resp.get("errorType")} Message: {resp.get("errorMessage")}')
        LOG.error(f'StackTrace: {resp.get("stackTrace")}')
        raise Exception(f'{resp["errorType"]}: {resp["errorMessage"]}')
    return resp


def random_string(length=8):
    return "".join(choice(ascii_lowercase) for _ in range(length))  # nosec B311


def put_function(sess, cluster_name):
    eks = sess.client("eks")
    try:
        eks_vpc_config = eks.describe_cluster(name=cluster_name)["cluster"][
            "resourcesVpcConfig"
        ]
    except eks.exceptions.ResourceNotFoundException:
        raise exceptions.InvalidRequest(f"cluster with name {cluster_name} not found")
    ec2 = sess.client("ec2")
    internal_subnets = [
        s["SubnetId"]
        for s in ec2.describe_subnets(
            SubnetIds=eks_vpc_config["subnetIds"],
            Filters=[
                {"Name": "tag-key", "Values": ["kubernetes.io/role/internal-elb"]}
            ],
        )["Subnets"]
    ]
    sts = sess.client("sts")
    role_arn = "/".join(
        sts.get_caller_identity()["Arn"]
        .replace(":sts:", ":iam:")
        .replace(":assumed-role/", ":role/")
        .split("/")[:-1]
    )
    lmbd = sess.client("lambda")
    function_config = {
        "FunctionName": f"awsqs-kubernetes-resource-proxy-{cluster_name}",
        "Runtime": "python3.8",
        "Role": role_arn,
        "Handler": "awsqs_kubernetes_resource.utils.proxy_wrap",
        "Timeout": 900,
        "MemorySize": 512,
        "VpcConfig": {
            "SubnetIds": internal_subnets,
            "SecurityGroupIds": eks_vpc_config["securityGroupIds"],
        }
    }
    try:
        no_update = update_function_config(lmbd, function_config)
    except lmbd.exceptions.ResourceNotFoundException:
        no_update = False
        with open("./awsqs_kubernetes_resource/vpc.zip", "rb") as zip_file:
            LOG.debug("Putting lambda function...")
            lmbd.create_function(Code={"ZipFile": zip_file.read()}, **function_config)
            LOG.debug("Done putting lambda function.")
    if not no_update:
        while not update_function_config(lmbd, function_config):
            time.sleep(5)


def update_function_config(lmbd, function_config):
    try:
        LOG.debug("Updating lambda function...")
        lmbd.update_function_configuration(**function_config)
        LOG.debug("Done updating lambda function...")
        return True
    except lmbd.exceptions.ResourceConflictException as e:
        if "The operation cannot be performed at this time." not in str(
                e
        ) and "The function could not be updated due to a concurrent update operation." not in str(
            e
        ) and "Conflict due to concurrent requests on this function." not in str(
            e
        ):
            raise
        return False


def delete_function(sess, cluster_name):
    lmbd = sess.client("lambda")
    try:
        lmbd.delete_function(FunctionName=f"awsqs-kubernetes-resource-proxy-{cluster_name}")
    except lmbd.exceptions.ResourceNotFoundException:
        LOG.warning("No cleanup performed, VPC lambda function does not exist")


def invoke_function(func_arn, event, sess):
    lmbd = sess.client("lambda")
    while True:
        try:
            response = lmbd.invoke(
                FunctionName=func_arn,
                InvocationType="RequestResponse",
                Payload=json.dumps(event).encode("utf-8"),
            )
            return json.loads(response["Payload"].read().decode("utf-8"))
        except lmbd.exceptions.ResourceConflictException as e:
            if "The operation cannot be performed at this time." not in str(e):
                raise
            LOG.error(str(e))
            time.sleep(10)
