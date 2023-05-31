import cfnresponse
import json
import logging
from random import choice
from string import ascii_uppercase, digits

logger = logging.getLogger(__name__)


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    logger.debug(json.dumps(event))

    status = cfnresponse.SUCCESS
    physical_resource_id = None

    try:
        if event["RequestType"] == "Create":
            physical_resource_id = "EKS-" + "".join(
                (choice(ascii_uppercase + digits) for i in range(8))  # nosec B311
            )
        else:
            physical_resource_id = event["PhysicalResourceId"]
    except Exception:
        logger.exception("Unhandled exception")
        status = cfnresponse.FAILED
    finally:
        cfnresponse.send(event, context, status, {}, physical_resource_id)
