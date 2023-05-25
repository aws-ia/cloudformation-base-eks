#!/bin/bash -xe
eval "$(pyenv init -)" &> /dev/null || true

pyenv shell eks-quickstart-dev || { echo 'Have you run "make dev" to setup your dev environment ?' ; exit 1 ; }

python docs/boilerplate/.utils/generate_parameter_tables.py
asciidoctor --base-dir docs/ --backend=html5 -o ../docs/index.html -w --doctype=book -a toc2 -a production_build docs/boilerplate/index.adoc
echo file://docs/index.html
