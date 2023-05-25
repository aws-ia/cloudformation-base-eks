FROM lambci/lambda:build-python3.7

COPY . .

RUN mkdir -p bin/ && \
    curl -o bin/rctl.tar.bz2 https://s3-us-west-2.amazonaws.com/rafay-prod-cli/publish/rctl-linux-amd64.tar.bz2 && \
    tar -C bin -xf bin/rctl.tar.bz2 && \
    chmod +x bin/rctl && \
    pip install -t python/ -r ./requirements.txt && \
    find . -name "*.dist-info"  -exec rm -rf {} \; | true && \
    find . -name "*.egg-info"  -exec rm -rf {} \; | true && \
    find . -name "*.pth"  -exec rm -rf {} \; | true && \
    find . -name "__pycache__"  -exec rm -rf {} \; | true && \
    rm bin/rctl.tar.bz2 Dockerfile requirements.txt && \
    find . -exec touch -t 202007010000.00 {} + && \
    zip -X -r lambda.zip ./

CMD mkdir -p /output/ && mv lambda.zip /output/
