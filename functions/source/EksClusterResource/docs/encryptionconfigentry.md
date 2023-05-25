# AWSQS::EKS::Cluster EncryptionConfigEntry

The encryption configuration for the cluster.

## Syntax

To declare this entity in your AWS CloudFormation template, use the following syntax:

### JSON

<pre>
{
    "<a href="#resources" title="Resources">Resources</a>" : <i>[ String, ... ]</i>,
    "<a href="#provider" title="Provider">Provider</a>" : <i><a href="provider.md">Provider</a></i>
}
</pre>

### YAML

<pre>
<a href="#resources" title="Resources">Resources</a>: <i>
      - String</i>
<a href="#provider" title="Provider">Provider</a>: <i><a href="provider.md">Provider</a></i>
</pre>

## Properties

#### Resources

Specifies the resources to be encrypted. The only supported value is "secrets".

_Required_: No

_Type_: List of String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### Provider

AWS Key Management Service (AWS KMS) customer master key (CMK). Either the ARN or the alias can be used.

_Required_: No

_Type_: <a href="provider.md">Provider</a>

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

