FROM lambci/lambda:build-python3.8

# https://docs.aws.amazon.com/eks/latest/userguide/install-kubectl.html
# N-1 version strategy for maximum interop coverage
ENV VERSION="1.23.15/2023-01-11"

COPY . /build

WORKDIR /build

RUN pip3 install -t apply/src --upgrade -r apply/requirements.txt && \
    find . -name "*.dist-info"  -exec rm -rf {} \; | true && \
    find . -name "*.egg-info"  -exec rm -rf {} \; | true && \
    find . -name "*.pth"  -exec rm -rf {} \; | true && \
    find . -name "__pycache__"  -exec rm -rf {} \; | true && \
    curl -o apply/src/bin/kubectl https://amazon-eks.s3-us-west-2.amazonaws.com/${VERSION}/bin/linux/amd64/kubectl && \
    curl -o apply/src/bin/aws-iam-authenticator https://amazon-eks.s3-us-west-2.amazonaws.com/${VERSION}/bin/linux/amd64/aws-iam-authenticator && \
    chmod +x apply/src/bin/kubectl && \
    chmod +x apply/src/bin/aws-iam-authenticator

RUN cd apply/src && \
    find . -exec touch -t 202007010000.00 {} + && \
    zip -Z bzip2 -r ../vpc.zip ./ && \
    cp ../vpc.zip /build/awsqs_kubernetes_apply_vpc.zip && \
    cp ../vpc.zip /build/awsqs_kubernetes_get_vpc.zip && \
    mv ../vpc.zip ./awsqs_kubernetes_resource/

RUN cd apply/src && zip -r -q ../ResourceProvider.zip ./ && \
    cd ../ && \
    mv awsqs-kubernetes-resource.json schema.json && \
    find . -exec touch -t 202007010000.00 {} + && \
    zip -r -q ../awsqs_kubernetes_apply.zip ./ResourceProvider.zip .rpdk-config schema.json inputs/

RUN pip3 install -t get/src --upgrade -r get/requirements.txt && \
    find . -name "*.dist-info"  -exec rm -rf {} \; | true && \
    find . -name "*.egg-info"  -exec rm -rf {} \; | true && \
    find . -name "*.pth"  -exec rm -rf {} \; | true && \
    find . -name "__pycache__"  -exec rm -rf {} \; | true && \
    cp -r ./apply/src/awsqs_kubernetes_resource ./get/src/ && \
    cp -p apply/src/bin/kubectl get/src/bin/ && \
    cp -p apply/src/bin/aws-iam-authenticator get/src/bin/

RUN cd get/src && zip -r -q ../ResourceProvider.zip ./ && \
    cd ../ && \
    mv awsqs-kubernetes-get.json schema.json && \
    find . -exec touch -t 202007010000.00 {} + && \
    zip -X -r -q ../awsqs_kubernetes_get.zip ./ResourceProvider.zip .rpdk-config schema.json inputs/

CMD mkdir -p /output/ && mv /build/*.zip /output/
