import boto3
import json
import logging
import re
from functools import partial

logger = logging.getLogger(__name__)


def template_iterator(obj, params, ssm, prefix):
    if isinstance(obj, dict):
        for k in obj:
            obj[k] = template_iterator(obj[k], params, ssm, prefix)
    elif isinstance(obj, list):
        for i, v in enumerate(obj):
            obj[i] = template_iterator(v, params, ssm, prefix)
    elif isinstance(obj, str):
        func = partial(resolver, ssm, prefix, params["params"])
        obj = re.sub(r"~~[\w/<>-]+~~", func, obj)
    return obj


def resolver(ssm, prefix, params, match):
    default = None
    param = match.group()[2:-2]
    if param.startswith("%"):
        return match.group()
    if "|" in param:
        default = "".join(param.split("|")[1:])
        param = param.split("|")[0]
    func = partial(param_resolve, params)
    param = re.sub(r"<\w+>", func, param)
    try:
        resp = ssm.get_parameter(Name=prefix + param)
        return json.loads(resp["Parameter"]["Value"])["Value"]
    except ssm.exceptions.ParameterNotFound:
        if default is None:
            raise Exception(f"Parameter {param} not found")
        return default


def param_resolve(params, match):
    return params[match.group()[1:-1]]


def handler(event, _):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    logger.debug(json.dumps(event))

    macro_response = {"requestId": event["requestId"], "status": "success"}

    try:
        ssm = boto3.client("ssm", region_name=event["region"])
        params = {
            "params": event["templateParameterValues"],
            "template": event["fragment"],
            "account_id": event["accountId"],
            "region": event["region"],
        }
        response = event["fragment"]
        prefix = (
            params["template"]
            .get("Mappings", {})
            .get("Config", {})
            .get("ParameterPrefix", {})
            .get("Value", "")
        )
        macro_response["fragment"] = template_iterator(response, params, ssm, prefix)
    except Exception as e:
        logger.exception("Unhandled exception")
        macro_response["status"] = "failure"
        macro_response["errorMessage"] = str(e)

    logger.info(json.dumps(macro_response))

    return macro_response
