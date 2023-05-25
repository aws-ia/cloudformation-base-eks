# AWSQS::EKS::Cluster Provider

AWS Key Management Service (AWS KMS) customer master key (CMK). Either the ARN or the alias can be used.

## Syntax

To declare this entity in your AWS CloudFormation template, use the following syntax:

### JSON

<pre>
{
    "<a href="#keyarn" title="KeyArn">KeyArn</a>" : <i>String</i>
}
</pre>

### YAML

<pre>
<a href="#keyarn" title="KeyArn">KeyArn</a>: <i>String</i>
</pre>

## Properties

#### KeyArn

Amazon Resource Name (ARN) or alias of the customer master key (CMK). The CMK must be symmetric, created in the same region as the cluster, and if the CMK was created in a different account, the user must have access to the CMK.

_Required_: No

_Type_: String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

