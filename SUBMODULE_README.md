# Re-using CloudFormation-Base-EKS as a component in your own project

This CloudFormation Reference Architecture is designed as a framework for deploying kubernetes based applications into Amazon EKS using AWS
CloudFormation. It can be used to provide:

* 2 possible entry points:
  * new vpc and EKS cluster
  * existing vpc new eks cluster
* Custom resource types:
  * KubeManifest - create, update, delete kubernetes resources using kubernetes manifests natively in CloudFormation.
  Can auto-generate names, and provides metadata in kubernetea api response as return values
  * Helm - use cloudformation to install applications using helm charts. supports custom repos, and passing values to
  charts. output includes release name, and names of all created resources.
* stabilise resources - wait for resources to complete before returning, this enables timing to be controlled between
dependent application components
* Usable as a submodule, base for kubernetes applications
* Provide a bastion host already configured with kubectl, helm and kubeconfig
* Creates EKS cluster and node group including a role that has access to the cluster and can be assumed by lambda

## Testing

1. create EC2 Keypair
1. Set `KeyPairName` as a [global taskcat override](https://aws-quickstart.github.io/input-files.html#parm-override)
1. deploy `amazon-eks-entrypoint-new-vpc.template.yaml` using taskcat `cd cloudformation-base-eks ; taskcat test run -t <EKS Version #> -l -n`
1. launch example workload template `example-workload.template.yaml`, needed parameters can be retrieved from outputs of
the entrypoint stack.
1. ssh into bastion host, validate that example `ConfigMap` (created by kubernetes manifest) and
`service-catalog` (created by helm install) have installed correctly

## Using as submodule

You can use the `amazon-eks-entrypoint-new-vpc.template.yaml`and `amazon-eks-entrypoint-existing-vpc.template.yaml` files as a starting point for building your own templates, updating the paths in both to point to the base eks submodule for all needed templates and adding a workload template to `amazon-eksentrypoint-existing-vpc.template.yaml` (can use `example-workload.template.yaml` as a starting point for this).
