# AWSQS::Kubernetes::Helm RepositoryOptions

Extra options for repository

## Syntax

To declare this entity in your AWS CloudFormation template, use the following syntax:

### JSON

<pre>
{
    "<a href="#username" title="Username">Username</a>" : <i>String</i>,
    "<a href="#password" title="Password">Password</a>" : <i>String</i>,
    "<a href="#cafile" title="CAFile">CAFile</a>" : <i>String</i>,
    "<a href="#insecureskiptlsverify" title="InsecureSkipTLSVerify">InsecureSkipTLSVerify</a>" : <i>Boolean</i>
}
</pre>

### YAML

<pre>
<a href="#username" title="Username">Username</a>: <i>String</i>
<a href="#password" title="Password">Password</a>: <i>String</i>
<a href="#cafile" title="CAFile">CAFile</a>: <i>String</i>
<a href="#insecureskiptlsverify" title="InsecureSkipTLSVerify">InsecureSkipTLSVerify</a>: <i>Boolean</i>
</pre>

## Properties

#### Username

Chart repository username

_Required_: No

_Type_: String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### Password

Chart repository password

_Required_: No

_Type_: String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### CAFile

Verify certificates of HTTPS-enabled servers using this CA bundle from S3

_Required_: No

_Type_: String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### InsecureSkipTLSVerify

Skip TLS certificate checks for the repository

_Required_: No

_Type_: Boolean

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

