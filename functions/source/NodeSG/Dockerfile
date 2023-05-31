FROM public.ecr.aws/sam/build-python3.9:latest

COPY ./index.py ./

RUN zip -X -r ./lambda.zip ./

CMD mkdir -p /output/ && mv lambda.zip /output/
