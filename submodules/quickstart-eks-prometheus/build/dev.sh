#!/bin/bash -xe

which pyenv &> /dev/null || { echo "pyenv must be installed. See https://github.com/pyenv/pyenv#installation" ; exit 1 ; }
which pyenv-virtualenv &> /dev/null || { echo "pyenv-virtualenv must be installed. See https://github.com/pyenv/pyenv-virtualenv#installation" ; exit 1 ; }
pyenv install 3.8.6 -s
pyenv virtualenv 3.8.6 eks-quickstart-dev || true
eval "$(pyenv init -)"
pyenv shell eks-quickstart-dev
pip install -qq --upgrade awscli taskcat cfn-lint git+https://github.com/aws-quickstart/qs-cfn-lint-rules.git
