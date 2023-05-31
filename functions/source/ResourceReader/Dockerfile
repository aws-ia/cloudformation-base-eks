FROM public.ecr.aws/sam/build-python3.9:latest

COPY ./index.py ./requirements.txt ./

RUN pip install -t ./ -r ./requirements.txt && \
    find . -name "*.dist-info"  -exec rm -rf {} \; | true && \
    find . -name "*.egg-info"  -exec rm -rf {} \; | true && \
    find . -name "*.pth"  -exec rm -rf {} \; | true && \
    find . -name "__pycache__"  -exec rm -rf {} \; | true && \
    curl -sSo awscliv2.zip https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip && \
    zip -X -r ./lambda.zip ./

CMD mkdir -p /output/ && mv ./lambda.zip /output/
