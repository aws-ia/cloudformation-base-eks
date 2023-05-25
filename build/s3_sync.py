#!/usr/bin/env python3

from sys import argv

import boto3
import logging

from taskcat._s3_sync import S3Sync, LOG

LOG.setLevel(logging.INFO)

bucket_name = argv[1]
bucket_region = argv[2]
bucket_profile = argv[3]
key_prefix = argv[4]
source_path = argv[5]
object_acl = argv[6]

client = boto3.Session(profile_name=bucket_profile).client('s3', region_name=bucket_region)

S3Sync(client, bucket_name, key_prefix, source_path, object_acl)
