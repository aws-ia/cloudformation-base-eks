import requests
import logging
import json
import random
import string
import boto3
from crhelper import CfnResource

logger = logging.getLogger(__name__)
helper = CfnResource(json_logging=True, log_level='DEBUG')

try:
    ssm_client = boto3.client('ssm')
except Exception as init_exception:
    helper.init_failure(init_exception)


def generate_password():
    password = ''.join(random.choice(string.ascii_letters + string.digits + string.punctuation) for i in range(8))
    return password


def create_rafay_org(email, org_name, first_name, last_name):
    try:
        endpoint = ssm_client.get_parameter(Name='/quickstart/rafay/endpoint')['Parameter']['Value']
    except ssm_client.exceptions.ParameterNotFound:
        endpoint = "rafay.dev"
    rafay_ops_console = f"ops.{endpoint}"
    rafay_console = f"console.{endpoint}"
    rafay_signup_url = "https://" + rafay_ops_console + \
                       "/auth/v1/signup/" \
                       "QVdTLVFTLVJBRkFZLVNJR05VUC1BUEktT1JJR0lOLUxBTUJEQS1VU0EtU1VOTllWQUxFLUNBLVZFUlNJT04tMDAwMQ/"
    rafay_login_url = "https://" + rafay_console + "/auth/v1/login/"
    rafay_users_url = "https://" + rafay_console + "/auth/v1/users/"
    # generate password for user
    user_password = generate_password()
    user_login_data = json.dumps({'username': email, 'password': user_password})
    signup_data = json.dumps({"username": email, "organization_name": org_name, "first_name": first_name,
                              "last_name": last_name, "password": user_password, "repeatPassword": user_password})

    # create new organization in Rafay SaaS controller
    r = requests.post(rafay_signup_url, headers={'content-type': 'application/json;charset=UTF-8'},
                      data=signup_data)
    assert_request(r, 'failed to create rafay org')

    # login to Rafay console
    r = requests.post(rafay_login_url, headers={'content-type': 'application/json;charset=UTF-8'},
                      data=user_login_data)
    assert_request(r, 'failed to login to rafay')
    user = requests.get(rafay_users_url,
                        headers={'content-type': 'application/json;charset=UTF-8',
                                 'cookie': 'rsid=' + r.cookies['rsid'] + ';csrftoken=' + r.cookies['csrftoken'],
                                 'x-csrftoken': r.cookies['csrftoken']})
    user_id = user.json()['users'][0]['account']['id']

    # get Rafay API key
    rafay_apikey_url = "https://" + rafay_console + "/auth/v1/users/" + user_id + "/apikey/"
    r = requests.post(rafay_apikey_url,
                      headers={'referer': "https://" + rafay_console,'content-type': 'application/json;charset=UTF-8',
                               'cookie': 'rsid=' + r.cookies['rsid'] + ';csrftoken=' + r.cookies['csrftoken'],
                               'x-csrftoken': r.cookies['csrftoken']},
                      data=json.dumps({"name": "dynamic"}))
    assert_request(r, 'failed to retreive api keys')
    return r.json()['key'], r.json()['secret']


def assert_request(r, msg):
    if r.status_code not in [200, 201]:
        try:
            code = r.json().get('details', [{}])[0].get('error_code')
            details = r.json().get('details', [{}])[0].get('detail', r.text)
        except Exception as e:
            logger.error(e, exc_info=True)
            code, details = ('', r.text)
        raise RuntimeError(f"{msg}: [{r.status_code} {r.reason}] {code} {details}")


@helper.create
def create(event, _):
    props = event['ResourceProperties']
    rafay_api_key, rafay_secret_key = create_rafay_org(props['email'], props['organization_name'],
                                                       props['first_name'],
                                                       props['last_name'])
    helper.Data['rafay_api_key'] = rafay_api_key
    helper.Data['rafay_secret_key'] = rafay_secret_key
    return rafay_api_key, rafay_secret_key


def lambda_handler(event, context):
    helper(event, context)
