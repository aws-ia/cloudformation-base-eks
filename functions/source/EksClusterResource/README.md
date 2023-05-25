# Retirement Notice
On 03/31/2023. Support for this Public Resource type will be retired. Please see [Issue #60](https://github.com/aws-quickstart/quickstart-amazon-eks-cluster-resource-provider/issues/60) for more information.


# AWSQS::EKS::Cluster


An AWS CloudFormation resource provider for modelling Amazon EKS clusters.
It provides some additional functionality to the native `AWS::EKS::Cluster` resource type:

* Manage `aws-auth` ConfigMap from within CloudFormation.
* Support for `EndpointPublicAccess`, `EndpointPrivateAccess` and
`PublicAccessCidrs` features.
* Support for enabling control plane logging to CloudWatch logs.
* Support for tagging

## Prerequisites

### IAM role
An IAM role is used by CloudFormation to execute the resource type handler code provided by this project. A CloudFormation template to create the execution role is available [here](https://github.com/aws-quickstart/quickstart-amazon-eks-cluster-resource-provider/blob/main/execution-role.template.yaml)

## Activating the Resource type
To activate the resource type in your account go [here](https://console.aws.amazon.com/cloudformation/home?region=us-east-1#/registry/public-extensions/details/schema?arn=arn:aws:cloudformation:us-east-1::type/resource/408988dff9e863704bcc72e7e13f8d645cee8311/AWSQS-EKS-Cluster), then choose the AWS Region you would like to use it in and click ***Activate***.

## Usage
Properties and return values are documented [here](https://github.com/aws-quickstart/quickstart-amazon-eks-cluster-resource-provider/blob/main/docs/README.md).

## Examples

### Create a private EKS cluster with an additional user and role allowed to access the Kubernetes API
```yaml
AWSTemplateFormatVersion: "2010-09-09"
Parameters:
  SubnetIds:
    Type: "List<AWS::EC2::Subnet::Id>"
  SecurityGroupIds:
    Type: "List<AWS::EC2::SecurityGroup::Id>"
Resources:
  # EKS Cluster
  myCluster:
    Type: "AWSQS::EKS::Cluster"
    Properties:
      RoleArn: !GetAtt serviceRole.Arn
      KubernetesNetworkConfig:
        ServiceIpv4Cidr: "192.168.0.0/16"
      ResourcesVpcConfig:
        SubnetIds: !Ref SubnetIds
        SecurityGroupIds: !Ref SecurityGroupIds
        EndpointPrivateAccess: true
        EndpointPublicAccess: false
      EnabledClusterLoggingTypes: ["audit"]
      KubernetesApiAccess:
        Users:
          - Arn: !Sub "arn:${AWS::Partition}:iam::${AWS::AccountId}:user/my-user"
            Username: "CliUser"
            Groups: ["system:masters"]
        Roles:
          - Arn: !Sub "arn:${AWS::Partition}:iam::${AWS::AccountId}:role/my-role"
            Username: "AdminRole"
            Groups: ["system:masters"]
      Tags:
        - Key: ClusterName
          Value: myCluster
  serviceRole:
    Type: AWS::IAM::Role
    Properties:
      AssumeRolePolicyDocument:
        Version: '2012-10-17'
        Statement:
          - Effect: Allow
            Principal: { Service: eks.amazonaws.com }
            Action: sts:AssumeRole
      Path: "/"
      ManagedPolicyArns:
        - !Sub 'arn:${AWS::Partition}:iam::aws:policy/AmazonEKSClusterPolicy'
        - !Sub 'arn:${AWS::Partition}:iam::aws:policy/AmazonEKSServicePolicy'
```

