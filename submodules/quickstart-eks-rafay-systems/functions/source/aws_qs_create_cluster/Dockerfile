FROM lambci/lambda:build-python3.7

COPY . .

RUN rm Dockerfile && \
    find . -exec touch -t 202007010000.00 {} + && \
    zip -X -r aws_qs_create_cluster.zip ./

CMD mkdir -p /output/ && mv aws_qs_create_cluster.zip /output/