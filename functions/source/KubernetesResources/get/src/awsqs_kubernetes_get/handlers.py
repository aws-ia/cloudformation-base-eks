import logging
from typing import Any, MutableMapping, Optional
import json
import base64
import time
from hashlib import md5
import boto3
import hashlib

from cloudformation_cli_python_lib import (
    Action,
    HandlerErrorCode,
    OperationStatus,
    ProgressEvent,
    Resource,
    SessionProxy,
    exceptions,
)

from .models import ResourceHandlerRequest, ResourceModel
from awsqs_kubernetes_resource.utils import create_kubeconfig, run_command
from awsqs_kubernetes_resource.vpc import proxy_needed, proxy_call, put_function, delete_function

# Use this logger to forward log messages to CloudWatch Logs.
LOG = logging.getLogger(__name__)
TYPE_NAME = "AWSQS::Kubernetes::Get"
LOG.setLevel(logging.DEBUG)


resource = Resource(TYPE_NAME, ResourceModel)
test_entrypoint = resource.test_entrypoint


def kubectl_get(model: ResourceModel, sess) -> ProgressEvent    :
    LOG.info('Received model: %s' % json.dumps(model._serialize()))
    if not proxy_needed(model.ClusterName, sess):
        create_kubeconfig(model.ClusterName, sess)
    model.Response = run_command(
        'kubectl get %s -o jsonpath="%s" --namespace %s' % (model.Name, model.JsonPath, model.Namespace),
        model.ClusterName,
        sess
    )
    LOG.info("returning progress...")
    return ProgressEvent(
        status=OperationStatus.SUCCESS,
        resourceModel=model,
    )


def encode_id(client_token: str, model: ResourceModel):
    return base64.b64encode(
        f'{client_token}|{model.ClusterName}|{model.Namespace}|{model.Name}|{model.JsonPath}'.encode('utf-8')
    ).decode("utf-8")


def decode_id(encoded_id, model):
    _, model.ClusterName, model.Namespace, model.Name, model.JsonPath = tuple(base64.b64decode(encoded_id).decode("utf-8").split("|"))


@resource.handler(Action.CREATE)
def create_handler(
    session: Optional[SessionProxy],
    request: ResourceHandlerRequest,
    callback_context: MutableMapping[str, Any],
) -> ProgressEvent:
    LOG.error("create handler invoked")
    model = request.desiredResourceState
    progress = ProgressEvent(
        status=OperationStatus.IN_PROGRESS,
        resourceModel=model,
    )
    if not callback_context:
        LOG.debug("1st invoke")
        model.Id = encode_id(request.clientRequestToken, model).replace('=', '')
        session.client('ssm').put_parameter(
            Name=f"/cloudformation-registry/awsqs-kubernetes-get/{model.Id}",
            Value=" ",
            Type='String'
        )
        progress.callbackDelaySeconds = 1
        progress.callbackContext = {"init": "complete"}
        return progress
    elif callback_context.get("init"):
        if proxy_needed(model.ClusterName, session):
            put_function(session, model.ClusterName)
        progress.callbackDelaySeconds = 1
        progress.callbackContext = {"retries": "0"}
        return progress
    elif callback_context.get("retries"):
        retries_done = int(callback_context.get("retries", "0"))
        retries_allowed = int(model.Retries) if model.Retries else 0
        try:
            read_handler(session, request, callback_context)
        except Exception as e:
            if retries_done >= retries_allowed:
                raise e
            progress.callbackDelaySeconds = 60
            progress.callbackContext = {"retries": str(retries_done + 1)}
            return progress
    return ProgressEvent(
        status=OperationStatus.SUCCESS,
        resourceModel=model,
    )


@resource.handler(Action.DELETE)
def delete_handler(
    session: Optional[SessionProxy],
    request: ResourceHandlerRequest,
    callback_context: MutableMapping[str, Any],
) -> ProgressEvent:
    LOG.error("delete handler invoked")
    model = request.desiredResourceState
    ssm = session.client('ssm')
    try:
        ssm.delete_parameter(Name=f"/cloudformation-registry/awsqs-kubernetes-get/{model.Id}")
    except ssm.exceptions.ParameterNotFound:
        raise exceptions.NotFound(TYPE_NAME, model.Id)
    return ProgressEvent(
        status=OperationStatus.SUCCESS,
        resourceModel=None,
    )


@resource.handler(Action.READ)
def read_handler(
    session: Optional[SessionProxy],
    request: ResourceHandlerRequest,
    callback_context: MutableMapping[str, Any],
) -> ProgressEvent:
    LOG.error("read handler invoked")
    model = request.desiredResourceState
    try:
        decode_id(model.Id + '===', model)
    except TypeError:
        raise exceptions.NotFound(TYPE_NAME, model.Id)
    ssm = session.client('ssm')
    try:
        ssm.get_parameter(Name=f"/cloudformation-registry/awsqs-kubernetes-get/{model.Id}")
    except ssm.exceptions.ParameterNotFound:
        raise exceptions.NotFound(TYPE_NAME, model.Id)
    return kubectl_get(model, session)
