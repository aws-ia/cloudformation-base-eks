# AWSQS::Kubernetes::Helm VPCConfiguration

For network connectivity to Cluster inside VPC

## Syntax

To declare this entity in your AWS CloudFormation template, use the following syntax:

### JSON

<pre>
{
    "<a href="#securitygroupids" title="SecurityGroupIds">SecurityGroupIds</a>" : <i>[ String, ... ]</i>,
    "<a href="#subnetids" title="SubnetIds">SubnetIds</a>" : <i>[ String, ... ]</i>
}
</pre>

### YAML

<pre>
<a href="#securitygroupids" title="SecurityGroupIds">SecurityGroupIds</a>: <i>
      - String</i>
<a href="#subnetids" title="SubnetIds">SubnetIds</a>: <i>
      - String</i>
</pre>

## Properties

#### SecurityGroupIds

Specify one or more security groups

_Required_: No

_Type_: List of String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### SubnetIds

Specify one or more subnets

_Required_: No

_Type_: List of String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

