FROM public.ecr.aws/sam/build-python3.9:latest

COPY ./requirements.txt ./

# https://docs.aws.amazon.com/lambda/latest/dg/configuration-layers.html#configuration-layers-path
RUN mkdir -p ./python/ && \
    pip install -t python/ -r ./requirements.txt && \
    find . -name "*.dist-info"  -exec rm -rf {} \; | true && \
    find . -name "*.egg-info"  -exec rm -rf {} \; | true && \
    find . -name "*.pth"  -exec rm -rf {} \; | true && \
    find . -name "__pycache__"  -exec rm -rf {} \; | true && \
    mv ./python/bin ./ && \
    mv ./python/* ./bin/ && \
    zip -X -r ./lambda.zip ./

CMD mkdir -p /output/ && mv ./lambda.zip /output/
