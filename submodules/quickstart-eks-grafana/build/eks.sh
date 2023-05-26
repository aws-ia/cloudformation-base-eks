#!/bin/bash -xe

eval "$(pyenv init -)" &> /dev/null || true

pyenv shell eks-quickstart-dev || { echo 'Have you run "make dev" to setup your dev environment ?' ; exit 1 ; }

# find account stack
ACC_STACK=Yes
for r in $(aws ec2 describe-regions --region ${AWS_REGION} --profile ${AWS_PROFILE} --output text --query Regions[].RegionName); do
  if [ "$(aws cloudformation describe-stacks --query 'Stacks[].{StackId: StackId, Version: Tags[?Key==`eks-quickstart`]|[0].Value}|[?Version==`AccountSharedResources`]|[0].StackId' --profile ${AWS_PROFILE} --region $r --output text)" != "None" ] ; then
    ACC_STACK=No
    break
  fi
done
REGION_STACK=Yes
if [ "$(aws cloudformation describe-stacks --query 'Stacks[].{StackId: StackId, Version: Tags[?Key==`eks-quickstart`]|[0].Value}|[?Version==`RegionalSharedResources`]|[0].StackId' --region ${AWS_REGION} --output text)" != "None" ] ; then
  REGION_STACK=No
fi

STACK_ID=$(aws cloudformation create-stack \
  --capabilities CAPABILITY_NAMED_IAM CAPABILITY_IAM CAPABILITY_AUTO_EXPAND \
  --stack-name $CLUSTER_NAME \
  --template-url https://aws-quickstart.s3.us-east-1.amazonaws.com/quickstart-amazon-eks/templates/amazon-eks-entrypoint-new-vpc.template.yaml \
  --parameters ParameterKey=EKSPublicAccessEndpoint,ParameterValue=Enabled \
  ParameterKey=ProvisionBastionHost,ParameterValue=Disabled \
  ParameterKey=RemoteAccessCIDR,ParameterValue=0.0.0.0/0 \
  ParameterKey=KeyPairName,ParameterValue=${AWS_KEYPAIR} \
  ParameterKey=AvailabilityZones,ParameterValue=\'"${AWS_REGION}a,${AWS_REGION}b,${AWS_REGION}c"\' \
  ParameterKey=PerAccountSharedResources,ParameterValue=$ACC_STACK \
  ParameterKey=PerRegionSharedResources,ParameterValue=$REGION_STACK \
  --query StackId --output text --profile ${AWS_PROFILE} --region ${AWS_REGION})

echo "wating for eks cluster to create, this can take up to 60 minutes..."
aws cloudformation wait stack-create-complete --stack-name $STACK_ID  --profile ${AWS_PROFILE} --region ${AWS_REGION}

aws cloudformation describe-stacks --stack-name $STACK_ID  --profile ${AWS_PROFILE} --region ${AWS_REGION} --query 'Stacks[0].Outputs[?OutputKey==`EKSClusterName`]' --output text
