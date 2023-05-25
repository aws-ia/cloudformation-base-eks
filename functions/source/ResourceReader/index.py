import json
import logging
import requests
import shlex
import subprocess  # nosec B404
from pathlib import Path
from time import sleep
from zipfile import ZipFile

logger = logging.getLogger(__name__)


def send(
    event,
    context,
    responseStatus,
    responseData,
    physicalResourceId=None,
    noEcho=False,
    reason="",
):
    responseUrl = event["ResponseURL"]

    logger.info(responseUrl)

    responseBody = {}
    responseBody["Status"] = responseStatus
    responseBody["Reason"] = (
        reason
        if reason
        else "See the details in CloudWatch Log Stream: " + context.log_stream_name
    )
    responseBody["PhysicalResourceId"] = physicalResourceId or context.log_stream_name
    responseBody["StackId"] = event["StackId"]
    responseBody["RequestId"] = event["RequestId"]
    responseBody["LogicalResourceId"] = event["LogicalResourceId"]
    responseBody["NoEcho"] = noEcho
    responseBody["Data"] = responseData

    json_responseBody = json.dumps(responseBody)

    logger.info("Response body:\n" + json_responseBody)

    headers = {"content-type": "", "content-length": str(len(json_responseBody))}

    try:
        response = requests.put(responseUrl, data=json_responseBody, headers=headers)
        logger.info("Status code: " + response.reason)
    except Exception as e:
        logger.exception("send(..) failed executing requests.put(..)")


def run_command(command):
    code = 0

    try:
        logger.debug("executing command: %s" % command)
        output = subprocess.check_output(  # nosec B603
            shlex.split(command), stderr=subprocess.STDOUT
        ).decode("utf-8")
        logger.debug(output)
    except subprocess.CalledProcessError as exc:
        code = exc.returncode
        output = exc.output.decode("utf-8")
        logger.error(
            "Command failed [exit %s]: %s"
            % (exc.returncode, exc.output.decode("utf-8"))
        )

    return code, output


with ZipFile("./awscliv2.zip") as zip:
    zip.extractall("/tmp/cli-install/")  # nosec B108

run_command("chmod +x /tmp/cli-install/aws/dist/aws")
run_command("chmod +x /tmp/cli-install/aws/install")
c, r = run_command("/tmp/cli-install/aws/install -b /tmp/bin -i /tmp/aws-cli")  # nosec B108

if c != 0:
    raise Exception(f"Failed to install cli. Code: {c} Message: {r}")


def execute_cli(properties):
    code, response = run_command(
        f"/tmp/bin/aws {properties['AwsCliCommand']} --output json"  # nosec B108
    )

    if code != 0 and ("NotFound" in response or "does not exist" in response):
        return None

    if code != 0:
        raise Exception(response)

    return json.loads(response)


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    logger.debug(json.dumps(event))

    status = "SUCCESS"
    pid = "None"
    resp = {}
    reason = ""

    try:
        if event["RequestType"] != "Delete":
            while not Path("/tmp/bin/aws").is_file():  # nosec B108
                logger.info("waiting for cli install to complete")
                sleep(10)

            resp = execute_cli(props)

            if "IdField" in props and isinstance(resp, dict):
                pid = resp[props["IdField"]]
            else:
                pid = str(resp)
    except Exception as e:
        logger.exception("Unhandled exception")
        reason = str(e)
        status = "FAILED"
    finally:
        send(event, context, status, resp, pid, reason=reason)
