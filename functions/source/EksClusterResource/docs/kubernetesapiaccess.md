# AWSQS::EKS::Cluster KubernetesApiAccess

## Syntax

To declare this entity in your AWS CloudFormation template, use the following syntax:

### JSON

<pre>
{
    "<a href="#roles" title="Roles">Roles</a>" : <i>[ <a href="kubernetesapiaccessentry.md">KubernetesApiAccessEntry</a>, ... ]</i>,
    "<a href="#users" title="Users">Users</a>" : <i>[ <a href="kubernetesapiaccessentry.md">KubernetesApiAccessEntry</a>, ... ]</i>
}
</pre>

### YAML

<pre>
<a href="#roles" title="Roles">Roles</a>: <i>
      - <a href="kubernetesapiaccessentry.md">KubernetesApiAccessEntry</a></i>
<a href="#users" title="Users">Users</a>: <i>
      - <a href="kubernetesapiaccessentry.md">KubernetesApiAccessEntry</a></i>
</pre>

## Properties

#### Roles

_Required_: No

_Type_: List of <a href="kubernetesapiaccessentry.md">KubernetesApiAccessEntry</a>

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### Users

_Required_: No

_Type_: List of <a href="kubernetesapiaccessentry.md">KubernetesApiAccessEntry</a>

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

