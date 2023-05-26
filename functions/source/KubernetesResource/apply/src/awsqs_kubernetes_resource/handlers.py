import logging
from typing import Any, MutableMapping, Optional
import re
from ruamel import yaml

from cloudformation_cli_python_lib import (
    Action,
    OperationStatus,
    ProgressEvent,
    Resource,
    SessionProxy,
    exceptions,
)

from .models import ResourceHandlerRequest, ResourceModel
from .utils import handler_init, encode_id, stabilize_job, run_command, get_model, build_model, create_kubeconfig, decode_id
from .vpc import proxy_needed, delete_function

# Use this logger to forward log messages to CloudWatch Logs.
LOG = logging.getLogger(__name__)
TYPE_NAME = "AWSQS::Kubernetes::Resource"
LOG.setLevel(logging.DEBUG)

resource = Resource(TYPE_NAME, ResourceModel)
test_entrypoint = resource.test_entrypoint

s3_scheme = re.compile(r"^s3://.+/.+")


@resource.handler(Action.CREATE)
def create_handler(
    session: Optional[SessionProxy],
    request: ResourceHandlerRequest,
    callback_context: MutableMapping[str, Any],
) -> ProgressEvent:
    model = request.desiredResourceState
    if not model.Namespace:
        raise exceptions.InvalidRequest("Namespace is required.")
    progress: ProgressEvent = ProgressEvent(
        status=OperationStatus.IN_PROGRESS, resourceModel=model,
    )
    LOG.debug(f"Create invoke \n\n{request.__dict__}\n\n{callback_context}")
    physical_resource_id, manifest_file, manifest_list = handler_init(
        model, session, request.logicalResourceIdentifier, request.clientRequestToken
    )
    model.CfnId = encode_id(
        request.clientRequestToken,
        model.ClusterName,
        model.Namespace,
        manifest_list[0]["kind"],
    )
    if not callback_context:
        LOG.debug("1st invoke")
        progress.callbackDelaySeconds = 1
        progress.callbackContext = {"init": "complete"}
        return progress
    if "stabilizing" in callback_context:
        if manifest_list[0]["apiVersion"].startswith("batch/") and manifest_list[0]["kind"] == 'Job':
            if stabilize_job(
                model.Namespace, callback_context["name"], model.ClusterName, session
            ):
                progress.status = OperationStatus.SUCCESS
            progress.callbackContext = callback_context
            progress.callbackDelaySeconds = 30
            LOG.debug(f"stabilizing: {progress.__dict__}")
            return progress
    try:
        # kubectl apply will update existing resources which breaks the
        # cfn registry contract. kubectl create fails when the resources in
        # your manifest already exist, which is what cfn expects.
        # https://docs.aws.amazon.com/cloudformation-cli/latest/userguide/resource-type-test-contract.html#resource-type-test-contract-additional-leaking
        cmd = f"kubectl create --save-config -o yaml -f {manifest_file}"
        if model.Namespace:
            cmd = f"{cmd} -n {model.Namespace}"
        outp = run_command(
            cmd,
            model.ClusterName,
            session,
        )
        build_model(list(yaml.safe_load_all(outp)), model)
    except Exception as e:
        if "Error from server (AlreadyExists)" not in str(e):
            raise
        LOG.debug("checking whether this is a duplicate request....")
        if not get_model(model, session):
            raise exceptions.AlreadyExists(TYPE_NAME, model.CfnId)
    if not model.Uid:
        # this is a multi-part resource, still need to work out stabilization for this
        pass
    elif manifest_list[0]["apiVersion"].startswith("batch/") and manifest_list[0]["kind"] == 'Job':
        callback_context["stabilizing"] = model.Uid
        callback_context["name"] = model.Name
        progress.callbackContext = callback_context
        progress.callbackDelaySeconds = 30
        LOG.debug(f"need to stabilize: {progress.__dict__}")
        return progress
    progress.status = OperationStatus.SUCCESS
    LOG.debug(f"success {progress.__dict__}")
    return progress


@resource.handler(Action.UPDATE)
def update_handler(
    session: Optional[SessionProxy],
    request: ResourceHandlerRequest,
    callback_context: MutableMapping[str, Any],
) -> ProgressEvent:
    model = request.desiredResourceState
    progress: ProgressEvent = ProgressEvent(
        status=OperationStatus.IN_PROGRESS, resourceModel=model,
    )
    if not model.CfnId:
        raise exceptions.NotFound(TYPE_NAME, model.Uid)
    if not proxy_needed(model.ClusterName, session):
        create_kubeconfig(model.ClusterName, session)
    token, cluster_name, namespace, kind = decode_id(model.CfnId)
    _p, manifest_file, _d = handler_init(
        model, session, request.logicalResourceIdentifier, token
    )        
    # validate if the resource is owned by this cloud formation resource
    if not callback_context:
        LOG.debug("update_handler validate the resource")
        if not get_model(model, session):
            raise exceptions.NotFound(TYPE_NAME, model.Uid)
        progress.callbackDelaySeconds = 5
        progress.callbackContext = {"validation": "complete"}
        return progress

    # resource ownership is already validated, update the resource
    if "validation" in callback_context:
        LOG.debug("update_handler update the resource")
        cmd = f"kubectl apply -o yaml -f {manifest_file}"
        if model.Namespace:
            cmd = f"{cmd} -n {model.Namespace}"
        outp = run_command(
            cmd,
            model.ClusterName,
            session,
        )
        build_model(list(yaml.safe_load_all(outp)), model)
        progress.status = OperationStatus.SUCCESS
        return progress
    else:
        LOG.debug("update_handler validation not present in the callback_context")
        progress.status = OperationStatus.FAILED
        return progress


@resource.handler(Action.DELETE)
def delete_handler(
    session: Optional[SessionProxy],
    request: ResourceHandlerRequest,
    callback_context: MutableMapping[str, Any],
) -> ProgressEvent:
    model = request.desiredResourceState
    progress: ProgressEvent = ProgressEvent(
        status=OperationStatus.SUCCESS, resourceModel=None,
    )
    if not model.CfnId:
        raise exceptions.InvalidRequest("CfnId is required.")
    if not proxy_needed(model.ClusterName, session):
        create_kubeconfig(model.ClusterName, session)
    _p, manifest_file, _d = handler_init(
        model, session, request.logicalResourceIdentifier, request.clientRequestToken
    )
    # validate if the resource is owned by this cloud formation resource
    if not callback_context:
        LOG.debug("delete_handler validate the resource")
        if not get_model(model, session):
            raise exceptions.NotFound(TYPE_NAME, model.Uid)
        progress.callbackDelaySeconds = 5
        progress.callbackContext = {"validation": "complete"}
        return progress
    # resource ownership is already validated, update the resource
    if "validation" in callback_context:
        LOG.debug("delete_handler delete the resource")
        try:
            cmd = f"kubectl delete -f {manifest_file}"
            if model.Namespace:
                cmd = f"{cmd} -n {model.Namespace}"
            run_command(
                cmd,
                model.ClusterName,
                session,
            )
        except Exception as e:
            if "Error from server (NotFound)" not in str(e):
                raise
        delete_function(session, model.ClusterName)
    return progress


@resource.handler(Action.READ)
def read_handler(
    session: Optional[SessionProxy],
    request: ResourceHandlerRequest,
    _callback_context: MutableMapping[str, Any],
) -> ProgressEvent:
    model = request.desiredResourceState
    if not model.CfnId:
        raise exceptions.NotFound(TYPE_NAME, model.Uid)
    if not proxy_needed(model.ClusterName, session):
        create_kubeconfig(model.ClusterName, session)
    if not get_model(model, session):
        raise exceptions.NotFound(TYPE_NAME, model.Uid)
    return ProgressEvent(status=OperationStatus.SUCCESS, resourceModel=model,)


@resource.handler(Action.LIST)
def list_handler(
    _session: Optional[SessionProxy],
    _request: ResourceHandlerRequest,
    _callback_context: MutableMapping[str, Any],
) -> ProgressEvent:
    raise NotImplementedError("List handler not implemented.")
