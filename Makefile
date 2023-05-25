.PHONY: build clean publish

TASKCAT_OPTIONS ?=
VERSION ?=
BUCKET ?=
PREFIX ?= quickstart-amazon-eks
PROFILE ?= default
REGION ?= $(shell aws configure get region --profile $(PROFILE))
GH_RELEASE ?= false
PART ?= patch
BUILD_FUNCTIONS ?= true
ACL ?= 'private'
RESOURCE_PATH ?=
RESOURCE_TYPE ?=
REGIONS ?=
TEST_NAMES ?=
QSPROD_TEST ?= false
REGIONAL_STACK ?= true
ACCOUNT_STACK ?= true
CLEAN_ACCOUNT ?= true
CLEAN_TYPES ?= true
CLEAN_REGIONAL ?= true

build:
	mkdir -p output/build/functions
	if [ "$(BUILD_FUNCTIONS)" == "true" ] ; then \
	  build/lambda_package.sh ; \
	fi
	cp -r functions/packages output/build/functions/
	cp -r scripts templates submodules output/build
	cp -r LICENSE.txt NOTICE.txt output/build
	if [ "$(VERSION)" != "" ] ; then \
	  sed -i "s|Default: $(PREFIX)/|Default: $(PREFIX)-versions/$(VERSION)/|g" output/build/templates/*.yaml ; \
	fi
	if [ "$(BUCKET)" != "" ] && [ $(QSPROD_TEST) == "true" ]; then \
 	  sed -i "s/UsingDefaultBucket: \!Equals \[\!Ref QSS3BucketName, 'aws-quickstart'\]/UsingDefaultBucket: \!Equals [\!Ref QSS3BucketName, \'$(BUCKET)\']/" output/build/templates/*.yaml ; \
	fi
	if [ "$(BUCKET)" != "" ] ; then \
	  sed -i "s/Default: aws-quickstart/Default: $(BUCKET)/" output/build/templates/*.yaml ; \
	fi
	if [ "$(REGION)" != "" ] ; then \
	  sed -i "s/Default: 'us-east-1'/Default: \'$(REGION)\'/" output/build/templates/*.yaml ; \
	fi
	cd output/build/ && \
	find . -exec touch -t 202007010000.00 {} + && \
	zip -X -r ../release.zip .

publish:
	if [ "$(BUCKET)" == "" ] ; then \
      echo BUCKET must be specified to publish; exit 1; \
    fi
	if [ "$(REGION)" == "" ] ; then \
      echo REGION must be specified to publish; exit 1; \
    fi
	if [ $(shell echo $(VERSION) | grep -c dev) -eq 0 ] ; then \
		if [ "$(GH_RELEASE)" == "true" ] ; then \
			hub release create -m v$(VERSION) -a "output/release.zip#$(PREFIX)-s3-package-v$(VERSION).zip" v$(VERSION) ;\
		fi ; \
	fi
	if [ "$(VERSION)" == "" ] ; then \
	  cd output/build && ../../build/s3_sync.py $(BUCKET) $(REGION) $(PROFILE) $(PREFIX)/ ./ $(ACL) ; \
	else \
	  cd output/build && ../../build/s3_sync.py $(BUCKET) $(REGION) $(PROFILE) $(PREFIX)-versions/ ./ $(ACL) ; \
	fi

register-resource:
	BUCKET=$(BUCKET) REGION=$(REGION) PROFILE=$(PROFILE) build/register-resource.sh

clean:
	rm -rf output/
	rm -rf taskcat_outputs
	rm -rf .taskcat
	rm -rf functions/packages

clean-aws:
	PROFILE=$(PROFILE) build/clean-aws.sh "$(REGIONS)"

taskcat:
	TASKCAT_GENERAL_S3_REGIONAL_BUCKETS=false PROFILE=$(PROFILE) TEST_NAMES=$(TEST_NAMES) REGIONS=$(REGIONS) taskcat -q test run -mnl --skip-upload

lint:
	cfn-lint templates/*.yaml