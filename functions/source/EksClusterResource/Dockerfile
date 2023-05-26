FROM public.ecr.aws/docker/library/golang:1.19-alpine

RUN apk --no-cache add py3-pip make git zip

RUN pip3 install cloudformation-cli-go-plugin

COPY . /build

WORKDIR /build

RUN GOPROXY=direct go mod download

RUN GOPROXY=direct make -f Makefile.package package

CMD mkdir -p /output/ && mv /build/awsqs-eks-cluster.zip /output/
