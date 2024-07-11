.PHONY: build package register

TYPE_NAME ?= AWSQS::EKS::Cluster
TYPE_NAME_LOWER ?= awsqs-eks-cluster
REGION ?= us-east-1
BUCKET ?= quickstart-resource-dev
EX_ROLE ?= use-existing
LOG_ROLE ?= use-existing
VERSION ?= $(shell cat version)
PUBLISHER_ID ?= 408988dff9e863704bcc72e7e13f8d645cee8311
WAIT ?= 1
SUM_FILES ?= $(TYPE_NAME_LOWER).json go.mod cmd/* vpc/* cmd/resource/*

build:
	go mod tidy
	cfn generate
	env CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -ldflags="-s -w" -tags="logging" -o bin/bootstrap2 cmd/main.go
	env CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -ldflags="-s -w" -o bin/bootstrap vpc/main.go

package: build
	find . -exec touch -t 202007010000.00 {} +
	cd bin ; zip -FS -X k8svpc.zip bootstrap ; rm bootstrap ; mv bootstrap2 bootstrap ; zip -X ../handler.zip ./k8svpc.zip ./bootstrap ; cd ..
	cp  awsqs-eks-cluster.json schema.json
	find . -exec touch -t 202007010000.00 {} +
	zip -Xr awsqs-eks-cluster.zip ./handler.zip ./schema.json ./.rpdk-config ./inputs
	rm ./handler.zip ./schema.json
	aws s3 cp ./awsqs-eks-cluster.zip s3://$(BUCKET)/

register:
	set -e ; \
    REGISTERED_SUM=`aws ssm get-parameter --name /cfn-registry/$(TYPE_NAME_LOWER)/sha --region $(REGION) --query Parameter.Value --output text || true` ; \
    CURRENT_SUM=`shasum $(SUM_FILES) 2> /dev/null | shasum - | awk '{print $$1}'` ; \
    if [ "$${REGISTERED_SUM}" == "$${CURRENT_SUM}" ] ; then echo "Current version is already registered"; exit 0 ; fi ; \
	R_LOG_ROLE=$(LOG_ROLE) ;\
	if [ "$(LOG_ROLE)" == "use-existing" ] ; then \
	  R_LOG_ROLE=`aws cloudformation describe-type --type RESOURCE --type-name "$(TYPE_NAME)" --region $(REGION) --output text --query LoggingConfig.LogRoleArn` ;\
    fi ;\
    R_EX_ROLE=$(EX_ROLE) ;\
    if [ "$(EX_ROLE)" == "use-existing" ] ; then \
	  R_EX_ROLE=`aws cloudformation describe-type --type RESOURCE --type-name "$(TYPE_NAME)" --region $(REGION) --output text --query ExecutionRoleArn` ;\
	fi ;\
	TOKEN=`aws cloudformation register-type \
		--type "RESOURCE" \
		--type-name  "$(TYPE_NAME)" \
		--schema-handler-package s3://$(BUCKET)/$(TYPE_NAME_LOWER).zip \
		--logging-config LogRoleArn=$${R_LOG_ROLE},LogGroupName=/cloudformation/registry/$(TYPE_NAME_LOWER) \
		--execution-role-arn $${R_EX_ROLE} \
		--region $(REGION) \
		--query RegistrationToken \
		--output text` ;\
	  ARN=`aws cloudformation describe-type-registration \
	    --registration-token $${TOKEN} \
	    --region $(REGION) --query TypeVersionArn --output text` ;\
	  if [ $(WAIT) -eq 1 ]; then \
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
			  break ; \
			fi ; \
			sleep 5 ; \
		  done ;\
		  aws cloudformation set-type-default-version --arn $${ARN} --region $(REGION) ; \
		  aws ssm put-parameter --name /cfn-registry/$(TYPE_NAME_LOWER)/sha --region $(REGION) --type String --value $${CURRENT_SUM} --overwrite ; \
	  else \
	    echo "REG_OUTPUT: $(REGION)~$${ARN}~$${TOKEN}~$${CURRENT_SUM}" ; \
      fi

register-all-regions:
	set -x ; \
	IN_PROGRESS="" ; \
	FAILED="" ; \
	ALL_REGIONS="$(shell aws ec2 describe-regions --query 'Regions[?OptInStatus!=`not-opted-in`].RegionName' --output text --region $(REGION))" ; \
    for r in $${ALL_REGIONS} ; do \
      REG_OUTP=`(BUCKET=$(BUCKET) REGION=$${r} EX_ROLE=$(EX_ROLE) LOG_ROLE=$(LOG_ROLE) WAIT=0 make register || FAILED="$${r} $${FAILED}") | grep '^REG_OUTPUT: ' | awk '{print $$2}'` ; \
      echo "REG_OUTP: $${REG_OUTP}" ; \
      if [ "$${REG_OUTP}" != "" ] ; then IN_PROGRESS="$${REG_OUTP} $${IN_PROGRESS}" ; fi ; \
	done ; \
	echo "IN_PROGRESS: \"$${IN_PROGRESS}\"" ;\
	for g in $${IN_PROGRESS}; do \
		r=`echo $${g} | awk -F~ '{print $$1}'` ; \
		ARN=`echo $${g} | awk -F~ '{print $$2}'` ; \
		TOKEN=`echo $${g} | awk -F~ '{print $$3}'` ; \
		SUM=`echo $${g} | awk -F~ '{print $$4}'` ; \
		while true; do \
  		  STATUS=`aws cloudformation describe-type-registration \
		    --registration-token $${TOKEN} \
		    --region $${r} --query ProgressStatus --output text` ;\
		  if [ "$${STATUS}" == "FAILED" ] ; then \
		    aws cloudformation describe-type-registration \
		    --registration-token $${TOKEN} \
		    --region $${r} ;\
		    FAILED="$${r} $${FAILED}" ; break ; \
		  fi ; \
		  if [ "$${STATUS}" == "COMPLETE" ] ; then \
		    aws cloudformation set-type-default-version --arn $${ARN} --region $${r} ; \
            aws ssm put-parameter --name /cfn-registry/$(TYPE_NAME_LOWER)/sha --region $${r} --type String --value $${SUM} --overwrite ; \
		    break ; \
		  fi ; \
		  sleep 5 ; \
	    done ; \
	done

contract-test:
	set -e ; \
	TYPE_ARN=`aws cloudformation describe-type --type RESOURCE --type-name $(TYPE_NAME) --region $(REGION) --query Arn --output text` ; \
	TEST_STATUS=`aws cloudformation describe-type --arn $${TYPE_ARN} --query TypeTestsStatus --output text  --region $(REGION)` ; \
	if [ "$${TEST_STATUS}" == "IN_PROGRESS" ] ; then echo "WARNING: $${TYPE_ARN} already has an IN_PROGRESS test, waiting for it to complete" ; \
	elif [ "$${TEST_STATUS}" == "PASSED" ] ; then echo "Contract tests already passed for $${TYPE_ARN}"; exit 0 ; \
	else aws cloudformation test-type --arn $${TYPE_ARN} --region $(REGION) --log-delivery-bucket $(BUCKET) ; TEST_STATUS=IN_PROGRESS ; fi ; \
	if [ "$${TEST_STATUS}" == "PASSED" ] ; then echo "Contract tests passed for $${TYPE_ARN}"; exit 0 ; \
	elif [ "$${TEST_STATUS}" == "IN_PROGRESS" ] ; then echo "Contract tests in progress for $${TYPE_ARN}"; exit 0 ; \
	else echo "Contract tests status $${TEST_STATUS} $${TYPE_ARN}"; exit 1 ; fi

contract-test-all-regions:
	ALL_REGIONS="$(shell aws ec2 describe-regions --query 'Regions[?OptInStatus!=`not-opted-in`].RegionName' --output text --region $(REGION))" ; \
	for r in $${ALL_REGIONS} ; do \
	  BUCKET=$(BUCKET) REGION=$${r} make contract-test ; \
	done ; \
	FAILED="" ; \
	for r in $${ALL_REGIONS} ; do \
	  TYPE_ARN=`aws cloudformation describe-type --type RESOURCE --type-name $(TYPE_NAME) --region $${r} --query Arn --output text` ; \
	  while [ "`aws cloudformation describe-type --arn $${TYPE_ARN} --query TypeTestsStatus --output text  --region $${r}`" == "IN_PROGRESS" ] ; do sleep 120 ; done ; \
      TEST_STATUS=`aws cloudformation describe-type --arn $${TYPE_ARN} --query TypeTestsStatus --output text  --region $${r}` ; \
      if [ "$${TEST_STATUS}" == "PASSED" ] ; then echo "Contract tests passed for $${TYPE_ARN}" ; \
      else echo "Contract tests status $${TEST_STATUS} $${TYPE_ARN}"; FAILED="$${TYPE_ARN} $${FAILED}" ; fi ; \
	done ; \
	if [ "$${FAILED}" != "" ] ; then echo "FAILED tests for $${FAILED}"; exit 1 ; fi

publish:
	aws cloudformation publish-type --type RESOURCE --type-name $(TYPE_NAME) --region $(REGION) --public-version-number $(VERSION)

publish-all-regions:
	REGIONS="" ; \
	ALL_REGIONS="$(shell aws ec2 describe-regions --query 'Regions[?OptInStatus!=`not-opted-in`].RegionName' --output text --region $(REGION))" ; \
	for r in $${ALL_REGIONS} ; do \
	  echo $${r} ; \
	  V=`aws cloudformation list-types --filters Category=THIRD_PARTY,TypeNamePrefix=$(TYPE_NAME),PublisherId=$(PUBLISHER_ID) --visibility PUBLIC --region $${r} --query 'TypeSummaries[0].LatestPublicVersion' --output text` ; \
      V=`echo $${V} | sed 's/None//g'` ;\
      if [ "`semver compare $(VERSION) $${V}`" -gt 0 ] ; then REGIONS="$${r} $${REGIONS}"; fi ; \
	done ; \
	FAILED="" ; \
    for r in $${REGIONS} ; do \
	  REGION=$${r} make publish || FAILED="$${r} $${FAILED}"; \
    done ;\
    if [ "$${FAILED}" != "" ] ; then echo "ERROR: failed to publish $(VERSION) in $${FAILED}" ; fi
