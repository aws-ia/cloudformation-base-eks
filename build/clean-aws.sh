#!/bin/bash -ex

REGIONS=${1}
TCAT_ARGS=""

if [ -z "${REGIONS}" ]
then
  REGIONS=$(aws ec2 describe-regions --query Regions[].RegionName --output text --profile ${PROFILE})
else
  TCAT_ARGS="-r ${REGIONS}"
fi

IAM_SLEEP=5
PARTITION=$(aws sts get-caller-identity --query Arn --output text | awk -F: '{print $2}')
RESOURCE_TYPES="AWSQS::EKS::Cluster AWSQS::Kubernetes::Helm AWSQS::Kubernetes::Resource AWSQS::Kubernetes::Get"

echo "cleaning quickstart stacks..."

taskcat -q test clean quickstart-amazon-eks -a ${PROFILE} ${TCAT_ARGS}

if ${CLEAN_STACKS}
then
  while [ true ]; do
     taskcat -q test list -p ${PROFILE} ${TCAT_ARGS} 2>&1 | grep 'quickstart-amazon-eks' || break
     clean=true
     for r in $(taskcat -q test list -p ${PROFILE} ${TCAT_ARGS} 2>&1 | grep 'quickstart-amazon-eks' | awk '{print $3}')
     do
       for s in $(aws cloudformation describe-stacks --query 'Stacks[? Tags[? Value==`quickstart-amazon-eks` && Key==`taskcat-project-name`] && ParentId == null].StackId' --region $r --output text --profile ${PROFILE})
       do
         if [ "$(aws cloudformation describe-stacks --stack-name $s --profile ${PROFILE} --region $r --query 'Stacks[0].StackStatus' --output text)" != "DELETE_FAILED" ]
         then
           clean=false
         else
           echo "WARNING: $s DELETE_FAILED"
         fi
       done
     done
     if $clean; then break ; fi
     sleep 30
  done
fi

if ${CLEAN_REGIONAL}
then
  echo "cleaning RegionalSharedResources stacks..."
  for r in $REGIONS
  do
    for s in $(aws cloudformation describe-stacks --query 'Stacks[? Tags[? Value==`RegionalSharedResources` && Key==`eks-quickstart`]].StackId' --region $r --output text --profile ${PROFILE})
    do
      echo "deleting $s"
      aws cloudformation delete-stack --stack-name $s --region $r --profile ${PROFILE}
    done
  done

  for r in $REGIONS
  do
    for s in $(aws cloudformation describe-stacks --query 'Stacks[? Tags[? Value==`RegionalSharedResources` && Key==`eks-quickstart`]].StackId' --region $r --output text --profile ${PROFILE})
    do
      aws cloudformation wait stack-delete-complete --stack-name $s --region $r --profile ${PROFILE}
    done
  done

  if ${CLEAN_TYPES}
  then
    echo "cleaning resource types..."
    for r in $REGIONS
    do
      for t in $RESOURCE_TYPES
      do
        for v in $(aws cloudformation list-type-versions --type-name $t --type RESOURCE --query TypeVersionSummaries[].Arn --output text --region $r --profile ${PROFILE} 2>/dev/null || true)
        do
          aws cloudformation deregister-type --arn $v --region $r --profile ${PROFILE} 2>/dev/null || \
            aws cloudformation deregister-type --type RESOURCE --type-name $t  --region $r --profile ${PROFILE}
        done
        RT=$(echo $t | tr [A-Z] [a-z] | sed 's/::/-/g')
        aws ssm delete-parameter --name /cfn-registry/${RT}/version --region $r --profile ${PROFILE} 2>/dev/null || true
      done
    done
  fi
fi

if ${CLEAN_ACCOUNT}
then
  echo "cleaning AccountSharedResources stacks..."
  for r in $REGIONS
  do
    for s in $(aws cloudformation describe-stacks --query 'Stacks[? Tags[? Value==`AccountSharedResources` && Key==`eks-quickstart`]].StackId' --region $r --output text --profile ${PROFILE})
    do
      echo "deleting $s"
      aws cloudformation delete-stack --stack-name $s --region $r --profile ${PROFILE}
    done
  done

  for r in $REGIONS
  do
    for s in $(aws cloudformation describe-stacks --query 'Stacks[? Tags[? Value==`AccountSharedResources` && Key==`eks-quickstart`]].StackId' --region $r --output text --profile ${PROFILE})
    do
      aws cloudformation wait stack-delete-complete --stack-name $s --region $r --profile ${PROFILE}
    done
  done

  echo "removing CloudFormation-Kubernetes-VPC role..."
  aws iam detach-role-policy  --role-name CloudFormation-Kubernetes-VPC  --policy-arn "arn:${PARTITION}:iam::aws:policy/service-role/AWSLambdaENIManagementAccess" --profile ${PROFILE} 2>/dev/null || true
  aws iam detach-role-policy  --role-name CloudFormation-Kubernetes-VPC  --policy-arn "arn:${PARTITION}:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole" --profile ${PROFILE} 2>/dev/null || true
  sleep ${IAM_SLEEP}
  aws iam delete-role --role-name CloudFormation-Kubernetes-VPC --profile ${PROFILE} 2>/dev/null || true
fi
