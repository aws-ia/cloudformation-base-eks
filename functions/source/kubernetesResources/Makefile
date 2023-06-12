.PHONY: build publish clean

REGION ?= us-east-1
BUCKET ?= jmmccon-uno-dev
EX_ROLE ?= arn:aws:iam::<ACCOUNT_ID>:role/<ROLE_NAME>
LOG_ROLE ?= arn:aws:iam::<ACCOUNT_ID>:role/<ROLE_NAME>

build:
	docker build . -t k8s-cfn-build
	docker run -i --name k8s-cfn-build k8s-cfn-build
	docker cp k8s-cfn-build:/output/. ./build/
	docker rm k8s-cfn-build

publish:
	set -ex ; \
    for n in apply get ; do \
        aws s3 cp ./build/awsqs_kubernetes_$${n}.zip s3://$(BUCKET)/ ;\
		TYPE_NAME=Resource ;\
        if [ "$${n}" == "get" ] ; then TYPE_NAME=Get ; fi ;\
		TOKEN=`aws cloudformation register-type \
			--type "RESOURCE" \
			--type-name  "AWSQS::Kubernetes::$${TYPE_NAME}" \
			--schema-handler-package s3://$(BUCKET)/awsqs_kubernetes_$${n}.zip \
			--logging-config LogRoleArn=$(LOG_ROLE),LogGroupName=/cloudformation/registry/awsqs-kubernetes-$${n} \
			--execution-role-arn $(EX_ROLE) \
			--region $(REGION) \
			--query RegistrationToken \
			--output text` ;\
		  while true; do \
			STATUS=`aws cloudformation describe-type-registration \
			  --registration-token $${TOKEN} \
			  --region $(REGION) --query ProgressStatus --output text` ;\
			if [ "$${STATUS}" == "FAILED" ] ; then \
			  aws cloudformation describe-type-registration \
			  --registration-token $${TOKEN} \
			  --region $(REGION) ;\
			  exit 1 ; \
			fi ; \
			if [ "$${STATUS}" == "COMPLETE" ] ; then \
			  ARN=`aws cloudformation describe-type-registration \
			  --registration-token $${TOKEN} \
			  --region $(REGION) --query TypeVersionArn --output text` ;\
			  break ; \
			fi ; \
			sleep 5 ; \
		  done ;\
		  aws cloudformation set-type-default-version --arn $${ARN} --region $(REGION) ;\
	done
clean:
	rm -rf build/
