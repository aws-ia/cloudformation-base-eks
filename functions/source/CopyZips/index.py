import cfnresponse
import logging
import boto3
import json

logger = logging.getLogger(__name__)


def copy_objects(source_bucket, dest_bucket, prefix, objects):
    s3 = boto3.client("s3")

    for o in objects:
        key = prefix + o
        copy_source = {"Bucket": source_bucket, "Key": key}
        logger.info(
            f"copy_source: {copy_source}\ndest_bucket: {dest_bucket}\nkey: {key}"
        )
        s3.copy_object(CopySource=copy_source, Bucket=dest_bucket, Key=key)


def delete_objects(bucket, prefix, objects):
    s3 = boto3.client("s3")
    objects = {"Objects": [{"Key": prefix + o} for o in objects]}

    try:
        logger.info(f'deleting objects: {objects["Objects"]}')
        resp = s3.delete_objects(Bucket=bucket, Delete=objects)
        logger.info(f"delete_objects response: {resp}")
    except s3.exceptions.NoSuchBucket:
        logger.debug(f"bucket: {bucket}, objects: {json.dumps(objects)}")
        pass


def handler(event, context):
    props = event.get("ResourceProperties", {})
    logger.setLevel(props.get("LogLevel", logging.INFO))

    logger.debug(json.dumps(event))

    status = cfnresponse.SUCCESS
    physical_resource_id = event.get("PhysicalResourceId", context.log_stream_name)

    try:
        if event["RequestType"] == "Delete":
            delete_objects(
                props["DestBucket"],
                props["Prefix"],
                props["Objects"],
            )
        elif event["RequestType"] == "Update":
            old_props = event["OldResourceProperties"]

            delete_objects(
                old_props["DestBucket"],
                old_props["Prefix"],
                old_props["Objects"],
            )

        if event["RequestType"] == "Create" or event["RequestType"] == "Update":
            copy_objects(
                props["SourceBucket"],
                props["DestBucket"],
                props["Prefix"],
                props["Objects"],
            )
    except Exception:
        logger.exception("Unhandled exception")
        status = cfnresponse.FAILED
    finally:
        cfnresponse.send(
            event,
            context,
            status,
            {},
            physical_resource_id,
        )
