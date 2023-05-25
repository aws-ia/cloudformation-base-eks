from datetime import datetime, date
import base64
import boto3
import json
import logging
from ruamel import yaml
import requests
import re
import shlex
import subprocess
import os
from pathlib import Path
from time import sleep
from cloudformation_cli_python_lib import exceptions
from .vpc import proxy_needed, put_function, proxy_call

LOG = logging.getLogger(__name__)
TYPE_NAME = "AWSQS::Kubernetes::Resource"
LOG.setLevel(logging.DEBUG)

s3_scheme = re.compile(r"^s3://.+/.+")


def s3_get(url, s3_client):
    try:
        return (
            s3_client.get_object(
                Bucket=url.split("/")[2], Key="/".join(url.split("/")[3:])
            )["Body"]
            .read()
            .decode("utf8")
        )
    except Exception as e:
        raise RuntimeError(f"Failed to fetch CustomValueYaml {url} from S3. {e}")


def http_get(url):
    try:
        response = requests.get(url)
    except requests.exceptions.RequestException as e:
        raise RuntimeError(f"Failed to fetch CustomValueYaml url {url}: {e}")
    if response.status_code != 200:
        raise RuntimeError(
            f"Failed to fetch CustomValueYaml url {url}: [{response.status_code}] "
            f"{response.reason}"
        )
    return response.text


def run_command(command, cluster_name, session):
    if cluster_name and session:
        if proxy_needed(cluster_name, session):
            put_function(session, cluster_name)
            manifest = None
            if Path("/tmp/manifest.yaml").is_file():
                with open("/tmp/manifest.yaml", "r") as fh:
                    manifest = fh.read()
            resp = proxy_call(cluster_name, manifest, command, session)
            log_output(resp)
            return resp
    retries = 0
    while True:
        try:
            try:
                LOG.debug("executing command: %s" % command)
                output = subprocess.check_output(
                    shlex.split(command), stderr=subprocess.STDOUT
                ).decode("utf-8")
                log_output(output)
            except subprocess.CalledProcessError as exc:
                LOG.error(
                    "Command failed with exit code %s, stderr: %s"
                    % (exc.returncode, exc.output.decode("utf-8"))
                )
                raise Exception(exc.output.decode("utf-8"))
            return output
        except Exception as e:
            if "Unable to connect to the server" not in str(e) or retries >= 5:
                raise
            LOG.debug("{}, retrying in 5 seconds".format(e))
            sleep(5)
            retries += 1


def create_kubeconfig(cluster_name, session=None):
    os.environ["PATH"] = f"/var/task/bin:{os.environ['PATH']}"
    os.environ["PYTHONPATH"] = f"/var/task:{os.environ.get('PYTHONPATH', '')}"
    os.environ["KUBECONFIG"] = "/tmp/kube.config"
    if session:
        creds = session.client.__self__.get_credentials()
        os.environ["AWS_ACCESS_KEY_ID"] = creds.access_key
        os.environ["AWS_SECRET_ACCESS_KEY"] = creds.secret_key
        os.environ["AWS_SESSION_TOKEN"] = creds.token
    run_command(
        f"aws eks update-kubeconfig --name {cluster_name} --alias {cluster_name} --kubeconfig /tmp/kube.config",
        None,
        None,
    )
    run_command(f"kubectl config use-context {cluster_name}", None, None)


def json_serial(o):
    if isinstance(o, (datetime, date)):
        return o.strftime("%Y-%m-%dT%H:%M:%SZ")
    raise TypeError("Object of type '%s' is not JSON serializable" % type(o))


def write_manifest(manifests, path):
    with open(path, 'w') as f:
        yaml.dump_all(manifests, f, default_style='"')


def generate_name(manifest, physical_resource_id, stack_name):
    if "metadata" in manifest.keys():
        if (
            "name" not in manifest["metadata"].keys()
            and "generateName" not in manifest["metadata"].keys()
        ):
            if physical_resource_id:
                manifest["metadata"]["name"] = physical_resource_id.split("/")[-1]
            else:
                manifest["metadata"]["generateName"] = "cfn-%s-" % stack_name.lower()
    return manifest


def build_model(kube_response, model):
    if len(kube_response) == 1:
        for key in ["uid", "selfLink", "resourceVersion", "namespace", "name"]:
            if key in kube_response[0]["metadata"].keys():
                setattr(
                    model, key[0].capitalize() + key[1:], kube_response[0]["metadata"][key]
                )


def handler_init(model, session, stack_name, token):
    LOG.debug(
        "Received model: %s" % json.dumps(model._serialize(), default=json_serial)
    )

    physical_resource_id = None
    manifest_file = "/tmp/manifest.yaml"
    if not proxy_needed(model.ClusterName, session):
        create_kubeconfig(model.ClusterName, session)
    s3_client = session.client("s3")
    if (not model.Manifest and not model.Url) or (model.Manifest and model.Url):
        raise Exception("Either Manifest or Url must be specified.")
    if model.SelfLink:
        physical_resource_id = model.SelfLink
    if model.Manifest:
        manifest_str = model.Manifest
    else:
        if re.match(s3_scheme, model.Url):
            manifest_str = s3_get(model.Url, s3_client)
        else:
            manifest_str = http_get(model.Url)
    manifests = []
    input_yaml = list(yaml.safe_load_all(manifest_str))
    for manifest in input_yaml:
        if len(input_yaml) == 1:
            generate_name(manifest, physical_resource_id, stack_name)
        add_idempotency_token(manifest, token)
        manifests.append(manifest)
    write_manifest(manifests, manifest_file)
    return physical_resource_id, manifest_file, manifests


def add_idempotency_token(manifest, token):
    if "metadata" not in manifest:
        manifest["metadata"] = {}
    if not manifest.get("metadata", {}).get("annotations"):
        manifest["metadata"]["annotations"] = {}
    manifest["metadata"]["annotations"]["cfn-client-token"] = token


def stabilize_job(namespace, name, cluster_name, session):
    cmd = f"kubectl get job/{name} -o yaml"
    if namespace:
        cmd = f"{cmd} -n {namespace}"
    response = yaml.safe_load_all(
        run_command(
            cmd, cluster_name, session
        )
    )
    for resource in response:
        # check for failures
        for condition in resource.get("status", {}).get("conditions", []):
            if condition.get("status") == "True":
                if condition.get("type") == "Failed":
                    raise exceptions.NotStabilized(f"Job failed {condition.get('reason')} {condition.get('message')}")
                if condition.get("type") != "Complete":
                    return False
        # check for success
        if resource.get("status", {}).get("succeeded"):
            return True
    # if it has not failed/succeeded, it is still in progress
    return False


def proxy_wrap(event, _context):
    LOG.debug(json.dumps(event))
    if event.get("manifest"):
        with open("/tmp/manifest.yaml", 'w') as f:
            f.write(event["manifest"])
        with open("/tmp/manifest.yaml", 'r') as f:
            LOG.debug(f.read())
    create_kubeconfig(event["cluster_name"])
    return run_command(event["command"], event["cluster_name"], boto3.session.Session())


def encode_id(client_token, cluster_name, namespace, kind):
    return base64.b64encode(
        f"{client_token}|{cluster_name}|{namespace}|{kind}".encode("utf-8")
    ).decode("utf-8")


def decode_id(encoded_id):
    return tuple(base64.b64decode(encoded_id).decode("utf-8").split("|"))


def get_model(model, session):
    token, cluster, namespace, kind = decode_id(model.CfnId)
    cmd = f"kubectl get {kind} -o yaml"
    if namespace:
        cmd = f"{cmd} -n {namespace}"
    outp = run_command(cmd, cluster, session)
    for i in yaml.safe_load(outp)["items"]:
        if token == i.get("metadata", {}).get("annotations", {}).get(
            "cfn-client-token"
        ):
            build_model([i], model)
            return model
    return None


def log_output(output):
    # CloudWatch PutEvents has a max length limit (256Kb)
    # Use slightly smaller value to include supporting information (timestamp, log level, etc.)
    limit = 260000
    output_string = f"{output}" # to support dictionaries as arguments
    for m in [output_string[i:i+limit] for i in range(0, len(output_string), limit)]:
        LOG.debug(m)
