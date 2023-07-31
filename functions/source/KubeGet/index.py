import boto3
import json
import logging
import math
import subprocess  # nosec B404
import shlex
import time
from hashlib import md5
from crhelper import CfnResource


logger = logging.getLogger(__name__)
helper = CfnResource(json_logging=True, log_level="DEBUG")

try:
    s3_client = boto3.client("s3")
    kms_client = boto3.client("kms")
except Exception as init_exception:
    helper.init_failure(init_exception)


def run_command(command):
    try:
        logger.info(f"executing command: {command}")
        output = subprocess.check_output(  # nosec B603
            shlex.split(command), stderr=subprocess.STDOUT
        ).decode("utf-8")
        logger.info(output)
    except subprocess.CalledProcessError as e:
        logger.exception(
            "Command failed with exit code %s, stderr: %s"
            % (e.returncode, e.output.decode("utf-8"))
        )
        raise Exception(e.output.decode("utf-8"))

    return output


def create_kubeconfig(cluster_name):
    run_command(
        f"aws eks update-kubeconfig --name {cluster_name} --alias {cluster_name}"
    )
    run_command(f"kubectl config use-context {cluster_name}")


@helper.create
@helper.update
def create_handler(event, context):
    create_kubeconfig(event["ResourceProperties"]["ClusterName"])

    props = event.get("ResourceProperties", {})
    name = props["Name"]
    interval = 5
    retry_timeout = (
        math.floor(context.get_remaining_time_in_millis() / interval / 1000) - 1
    )

    namespace = props["Namespace"]
    json_path = props["JsonPath"]

    while True:
        try:
            outp = run_command(
                f'kubectl get {name} -o jsonpath="{json_path}" --namespace {namespace}'
            )
            break
        except Exception:
            if retry_timeout < 1:
                message = "Out of retries"
                logger.error(message)
                raise RuntimeError(message)
            else:
                logger.info("Retrying until timeout...")

                time.sleep(interval)
                retry_timeout = retry_timeout - interval

    response_data = {"id": ""}

    if "ResponseKey" in event["ResourceProperties"]:
        response_data[event["ResourceProperties"]["ResponseKey"]] = outp

    if len(outp.encode("utf-8")) > 1000:
        outp_utf8 = outp.encode("utf-8")
        md5_digest = md5(outp_utf8).hexdigest()  # nosec B324, B303
        outp = "MD5-" + str(md5_digest)

    helper.Data.update(response_data)

    return outp


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    logger.debug(json.dumps(event))

    helper(event, context)
