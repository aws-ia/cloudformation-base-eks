import logging
import boto3
from crhelper import CfnResource

logger = logging.getLogger(__name__)
helper = CfnResource(json_logging=True, log_level='DEBUG')

try:
    eks_client = boto3.client('eks')
except Exception as init_exception:
    helper.init_failure(init_exception)

@helper.create
@helper.update
def create(event, _):
    response = eks_client.describe_nodegroup(
        clusterName=event['ResourceProperties']['ClusterName'],
        nodegroupName=event['ResourceProperties']['NodeGroupName']
    )
    return response['nodegroup']['resources']['remoteAccessSecurityGroup']

def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))
    helper(event, context)
