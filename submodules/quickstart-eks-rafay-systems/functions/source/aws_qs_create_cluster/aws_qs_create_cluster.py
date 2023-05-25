import logging
import subprocess
import os
import time
import boto3
from hashlib import sha1
from crhelper import CfnResource

logger = logging.getLogger(__name__)
helper = CfnResource(json_logging=True, log_level='DEBUG')

try:
    s3_client = boto3.client('s3')
    ssm_client = boto3.client('ssm')
except Exception as init_exception:
    helper.init_failure(init_exception)


def create_rafay_cluster(api_key, api_secret, rafay_project, rafay_cluster_name, s3_bucket, s3_key):
    if len(rafay_cluster_name) > 30:
        rafay_cluster_name = rafay_cluster_name[:21] + '-' + sha1(rafay_cluster_name.encode('utf-8')).hexdigest()[:8]
    rafay_cluster_name = rafay_cluster_name.lower()
    os.environ["RCTL_API_KEY"] = api_key
    os.environ["RCTL_API_SECRET"] = api_secret
    os.environ["RCTL_PROJECT"] = rafay_project
    try:
        endpoint = ssm_client.get_parameter(Name='/quickstart/rafay/endpoint')['Parameter']['Value']
    except ssm_client.exceptions.ParameterNotFound:
        endpoint = "rafay.dev"
    os.environ["RCTL_REST_ENDPOINT"] = f"console.{endpoint}"
    rctl_cluster_name = rafay_cluster_name
    file_path = '/tmp/' + rctl_cluster_name + '-bootstrap.yaml'
    # create an imported cluster in Rafay to get bootstrap configuration 
    cluster_cmd = "rctl create cluster imported " + rctl_cluster_name + " -l aws/" + os.environ["AWS_REGION"] + \
                  " > " + file_path
    try:
        subprocess.check_output(cluster_cmd, shell=True, stderr=subprocess.STDOUT)
    except subprocess.CalledProcessError as e:
        logger.error(f"rctl command call failed: \n\n{e.output}")
    with open(file_path) as f:
        manifest = f.read()
        if 'cluster.rafay.dev' in manifest:
            s3_client.upload_file(file_path, s3_bucket, s3_key)
            time.sleep(30)
            return s3_bucket, s3_key
        else:
            raise RuntimeError(f"cluster creation failed: {manifest}")


@helper.create
def create(event, _):
    props = event['ResourceProperties']
    s3_bucket, s3_key = create_rafay_cluster(props['RAFAY_API_KEY'], props['RAFAY_API_SECRET'],
                                             props['RAFAY_PROJECT'],
                                             props['RAFAY_CLUSTER_NAME'], props['s3_bucket'], props['s3_key'])
    helper.Data['rafay_s3_bucket'] = s3_bucket
    helper.Data['rafay_s3_key'] = s3_key
    return s3_bucket, s3_key


@helper.delete
def delete(event, _):
    props = event['ResourceProperties']
    s3_client.delete_object(Bucket=props['s3_bucket'], Key=props['s3_key'])


def lambda_handler(event, context):
    helper(event, context)
