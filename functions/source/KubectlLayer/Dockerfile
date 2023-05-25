FROM public.ecr.aws/sam/build-python3.9:latest

# https://docs.aws.amazon.com/eks/latest/userguide/install-kubectl.html
# N-1 version strategy for maximum interop coverage
ENV VERSION="1.23.15/2023-01-11"

RUN mkdir ./bin/ && \
    curl -sSo ./bin/kubectl https://amazon-eks.s3-us-west-2.amazonaws.com/${VERSION}/bin/linux/amd64/kubectl && \
    curl -sSo ./bin/aws-iam-authenticator https://amazon-eks.s3-us-west-2.amazonaws.com/${VERSION}/bin/linux/amd64/aws-iam-authenticator && \
    chmod +x ./bin/kubectl && \
    chmod +x ./bin/aws-iam-authenticator && \
    zip -X -r ./lambda.zip ./

CMD mkdir -p /output/ && mv ./lambda.zip /output/
