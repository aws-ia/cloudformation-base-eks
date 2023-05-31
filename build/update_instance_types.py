#!/usr/bin/env python3

import json
import re
from pathlib import Path
from sys import argv
import boto3
import requests
import yaml
from cfnlint.decode import cfn_yaml
from cfn_flip import load_yaml, get_dumper

TASKCAT_GLOBAL_CONFIG = Path('~/.taskcat.yml').expanduser().resolve()
TASKCAT_PROJECT_CONFIG = Path('./.taskcat.yml').resolve()
INSTANCE_INFO = 'https://ec2instances.info/instances.json'


def dump_yaml(data, clean_up=False, long_form=False):
    """
    Output some YAML
    """

    return yaml.dump(
        data,
        Dumper=get_dumper(clean_up, long_form),
        default_flow_style=False,
        allow_unicode=True
    )


def get_region_map():
    auth_map = {}
    if TASKCAT_GLOBAL_CONFIG.exists():
        with open(TASKCAT_GLOBAL_CONFIG, 'r') as fh:
            auth_map.update(yaml.safe_load(fh).get('general', {}).get('auth', {'default': 'default'}))
    if 'default' not in auth_map:
        auth_map['default'] = 'default'
    ec2 = boto3.Session(profile_name=auth_map['default']).client('ec2')
    regions = get_qs_regions(ec2, auth_map)
    return regions


def get_qs_regions(ec2, auth_map):
    if not TASKCAT_PROJECT_CONFIG.exists():
        return {r['RegionName']: auth_map.get(r['RegionName'], auth_map['default']) for r in ec2.describe_regions()['Regions'] if r['OptInStatus'] != 'not-opted-in'}
    with open(TASKCAT_PROJECT_CONFIG, 'r') as fh:
        config = yaml.safe_load(fh)
        regions = set(config.get('project', {}).get('regions', []))
        for t in config.get('tests', {}).values():
            regions.update(set(t.get('regions', {})))
        return {r: auth_map.get(r, auth_map['default']) for r in regions}


def template_rewriter(pointer_range, new_string, template):
    template = template.start_mark.buffer
    if template.endswith('\0'):
        template = template[:-1]
    start, end = pointer_range
    length = end-start
    template_str = template[:start] + new_string + template[start+length:]
    return cfn_yaml.loads(template_str)


def get_instances(filters, auth_map):
    profile = auth_map.get('default', 'default')
    ec2_api_response = []
    paginator = boto3.Session(profile_name=profile).client('ec2').get_paginator('describe_instance_types')
    for page in paginator.paginate():
        ec2_api_response.extend(page['InstanceTypes'])
    raw_instances = {i['InstanceType']: i for i in ec2_api_response}
    ec2info = requests.get(INSTANCE_INFO).json()
    for i in ec2info:
        if i['instance_type'] in raw_instances:
            raw_instances[i['instance_type']].update(i)
        else:
            raw_instances[i['instance_type']] = i
    instance_prices = {}
    region_map = {}
    add_end = []
    for ec2_filter in filters:
        raw_instances = eval_filter(ec2_filter, raw_instances)
    for r in raw_instances.values():
        price = r.get('pricing', {}).get('us-east-1', {}).get('linux', {}).get('ondemand')
        if price:
            instance_regions = set(r['pricing'].keys())
            for region_name in instance_regions:
                if region_name not in region_map:
                    region_map[region_name] = [r['instance_type']]
                else:
                    region_map[region_name].append(r['instance_type'])
            instance_prices[r['instance_type']] = price
        else:
            i = r['instance_type'] if 'instance_type' in r else r.get('InstanceType')
            if i:
                add_end.append(i)
    instance_prices = [k for k, v in sorted(instance_prices.items(), key=lambda item: item[1])] + add_end
    return instance_prices, region_map


def eval_filter(ec2_filter, instances):
    value, condition, key = ec2_filter
    matched_instances = {}
    for instance_type, instance in instances.items():
        instance_key = instance.copy()
        no_key = False
        for k in key.split('.'):
            if k not in instance_key:
                no_key = True
                break
            instance_key = instance_key[k]
        actual = json.loads(json.dumps(instance_key))
        if isinstance(actual, str):
            actual = f'"{actual}"'
        if isinstance(value, (bool, int, list)):
            rendered_value = str(value)
        elif value.isdigit() or value in ["True", "False"]:
            rendered_value = value
        else:
            rendered_value = f'"""{value}"""'
        stmnt = f'{rendered_value} {condition} {actual}'
        if no_key:
            t = instance['instance_type'] if 'instance_type' in instance else instance.get('InstanceType', instance)
            # print(f"dropping {t} because it has no key matching {key}")
        elif eval(stmnt, {}, {}) is True:
            matched_instances[instance_type] = instance
        # elif instance_type not in matched_instances:
            # print(f'{instance_type} does not match `{rendered_value} {condition} {actual}`')
    return matched_instances


if __name__ == '__main__':
    if not len(argv) == 2:
        print("Usage: update_instance_types.py <TEMPLATE_PATH>")
        exit(1)
    template_path = Path(argv[1]).expanduser().resolve()
    if not template_path.is_file():
        print(f"Cannot find template at {template_path}")
        exit(1)
    template = cfn_yaml.load(str(template_path))
    config = template.get('Metadata', {}).get('AutoInstance')
    if not config:
        print(f"Config not present in template at Metadata->AutoInstance")
        exit(1)
    for parameter, param_config in config.items():
        print(f"processing {parameter}")
        param = template.get('Parameters', {}).get(parameter)
        if not param:
            print(f"Cannot find parameter {parameter} in template at {template}")
            exit(1)
        auth_map = get_region_map()
        filters = template['Metadata']['AutoInstance'][parameter].get('InstanceFilters', [])
        ordered_instances, region_support_map = get_instances(filters, auth_map)
        if param.get('AllowedValues') is not None:
            print("adding values")
            start = param['AllowedValues'].start_mark.index
            end = param['AllowedValues'].end_mark.index
            template = template_rewriter((start, end), json.dumps(ordered_instances), template)
        if template.get('Rules'):
            rules = load_yaml(template.start_mark.buffer[template['Rules'].start_mark.index-2:template['Rules'].end_mark.index])
            for region_name, instances in region_support_map.items():
                rule_name = f"{parameter}{region_name.replace('-', '').capitalize()}Instances"
                rules[rule_name] = {
                    "RuleCondition": {"Fn::Equals": [{"Ref": "AWS::Region"}, region_name]},
                    "Assertions": [
                        {
                            "Assert":  {"Fn::Contains": [instances, {"Ref": str(parameter)}]},
                            "AssertDescription": f"Valid instance types for {region_name} are: {instances}"
                        }
                    ]
                }
            rules = dump_yaml({"Rules": rules})
            snippet = (template['Rules'].start_mark.index-9, template['Rules'].end_mark.index)
            template = template_rewriter(snippet, rules, template)
        with open(template_path, 'w') as fh:
            fh.write(template.start_mark.buffer[:-1])
