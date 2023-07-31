#  Copyright 2016 Amazon Web Services, Inc. or its affiliates. All Rights Reserved.
#  This file is licensed to you under the AWS Customer Agreement (the "License").
#  You may not use this file except in compliance with the License.
#  A copy of the License is located at http://aws.amazon.com/agreement/ .
#  This file is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, express or implied.
#  See the License for the specific language governing permissions and limitations under the License.

import boto3
import json
import logging
import re

# Provided through CrhelperLayer in amazon-eks-per-region-resources.template.yaml
from crhelper import CfnResource
from time import sleep

logger = logging.getLogger(__name__)

ec2 = boto3.client("ec2")
helper = CfnResource(json_logging=True, log_level="DEBUG")


def get_attachment_id_for_eni(eni):
    try:
        return eni["Attachment"]["AttachmentId"]
    except KeyError:
        return None


def delete_dependencies(sg_id, security_groups):
    complete = True

    logger.info(f"Deleting dependencies for {sg_id}...")

    for sg in security_groups["SecurityGroups"]:
        for p in sg["IpPermissions"]:
            if "UserIdGroupPairs" in p.keys():
                if sg_id in [x["GroupId"] for x in p["UserIdGroupPairs"]]:
                    try:
                        logger.debug(
                            "Revoking ingress rule %s from %s..." % (p, sg["GroupId"])
                        )
                        ec2.revoke_security_group_ingress(
                            GroupId=sg["GroupId"], IpPermissions=[p]
                        )
                        logger.debug(
                            "Revoked ingress rule %s from %s." % (p, sg["GroupId"])
                        )
                    except Exception:
                        complete = False
                        logger.exception(
                            "ERROR: Failed to revoke ingress rule %s from %s"
                            % (p, sg["GroupId"])
                        )

                        continue

    for sg in security_groups["SecurityGroups"]:
        for p in sg["IpPermissionsEgress"]:
            if "UserIdGroupPairs" in p.keys():
                if sg_id in [x["GroupId"] for x in p["UserIdGroupPairs"]]:
                    try:
                        logger.debug(
                            "Revoking egress rule %s from %s..." % (p, sg["GroupId"])
                        )
                        ec2.revoke_security_group_egress(
                            GroupId=sg["GroupId"], IpPermissions=[p]
                        )
                        logger.debug(
                            "Revoked egress rule %s from %s." % (p, sg["GroupId"])
                        )
                    except Exception:
                        complete = False
                        logger.exception(
                            "ERROR: Failed to revoke ingress rule %s from %s"
                            % (sg["GroupId"])
                        )

                        continue

    filters = [{"Name": "group-id", "Values": [sg_id]}]
    for eni in ec2.describe_network_interfaces(Filters=filters)["NetworkInterfaces"]:
        try:
            attachment_id = get_attachment_id_for_eni(eni)
            if attachment_id:
                logger.debug(
                    "Detaching ENI %s from %s..." % (eni["NetworkInterfaceId"], sg_id)
                )
                ec2.detach_network_interface(AttachmentId=attachment_id, Force=True)
                logger.info(
                    "Detached ENI %s from %s." % (eni["NetworkInterfaceId"], sg_id)
                )

                sleep(5)

            logger.debug("Deleting ENI %s..." % (eni["NetworkInterfaceId"]))
            ec2.delete_network_interface(NetworkInterfaceId=eni["NetworkInterfaceId"])
            logger.info("Deleted ENI %s." % (eni["NetworkInterfaceId"]))
        except Exception:
            complete = False
            logger.exception("ERROR: %s" % (eni["NetworkInterfaceId"]))

            continue

    return complete


@helper.delete
def delete_handler(event, context):
    for sg_id in event.get("ResourceProperties", {}).get("SecurityGroups", {}):
        interval = 15  # seconds

        if not re.match(r"^sg-(?:[0-9a-f]{8}|[0-9a-f]{17})$", sg_id):
            message = f"ERROR: Invalid security group ID: {sg_id}."
            if len(str(sg_id)) == 1:
                message += (
                    " The SecurityGroups property appears to be configured " +
                    "as a string instead of a list."
                )
            logger.error(message)
            raise ValueError(message)

        while context.get_remaining_time_in_millis() > (interval * 1000):
            try:
                logger.debug(f"Querying security group {sg_id}...")
                security_groups = ec2.describe_security_groups(GroupIds=[sg_id])
                logger.info(f"Found security group {sg_id}...")
            except:
                logger.warning(f"{sg_id} not found. Skipping...")

                break

            if delete_dependencies(sg_id, security_groups):
                try:
                    logger.debug(f"Deleting security group {sg_id}...")
                    ec2.delete_security_group(GroupId=sg_id)
                    logger.info(f"Deleted security group {sg_id}.")
                except Exception:
                    logger.exception(f"ERROR: Failed to delete {sg_id}.")

                    if context.get_remaining_time_in_millis() <= (interval * 1000):
                        message = f"ERROR: Out of retries deleting {sg_id}."
                        logger.error(message)

                        # raise RuntimeError(message)
                        pass
                    else:
                        sleep(interval)

                        continue

            elif context.get_remaining_time_in_millis() <= (interval * 1000):
                message = f"ERROR: Out of retries deleting {sg_id} dependencies."
                logger.error(message)

                # raise RuntimeError(message)
            else:
                logger.error(
                    f"ERROR: Failed to delete {sg_id} dependencies. Retrying..."
                )

                sleep(interval)

        logger.info(f"Processed {sg_id} successfully.")

    logger.info(f"Processed delete event successfully.")


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    logger.debug(json.dumps(event))

    helper(event, context)
