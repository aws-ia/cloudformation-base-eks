#!/bin/bash

if [ "PROJECT_NAME" == "" ] ; then
  echo 'PROJECT_NAME must be specified to initialize project:'; echo ; echo 'PROJECT_NAME="Company Product" make init'
  exit 1
fi

for f in $(find . -exec file {} \; | grep text | cut -d: -f1 | grep -v '^\./\.git\|^\./docs/generated\|^\./docs/boilerplate\|^\./\.idea\|^\./build\|^\./Makefile') ; do
  sed -i "s/PPNAMELD/$PPNAMELD/g" "$f"
  sed -i "s/PPNAMEC/$PPNAMEC/g" "$f"
  sed -i "s/PPNAME/$PROJECT_NAME/g" "$f"
done

git mv templates/eks-PPNAMELD.template.yaml templates/eks-${PPNAMELD}.template.yaml
sed -i 's/- \[ \] init project /- [x] init project /' README.md
