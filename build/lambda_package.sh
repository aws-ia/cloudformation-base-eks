#!/bin/sh -e
ONLY=$1
cd functions/source/
for d in * ; do
    if [ "${ONLY}" == "" ] ; then  CUR_RESOURCE_PATH=$d ; else CUR_RESOURCE_PATH=$ONLY; fi
    if [ "${CUR_RESOURCE_PATH}" == "$d" ] ; then
      n=$(echo $d| tr '[:upper:]' '[:lower:]')
      cd $d
      if [ -z "$ECR_BUILD_CACHE" ]; then
        docker build -t $n .
      else
        docker build --cache-from ${ECR_BUILD_CACHE}:$n -t $n .
      fi
      docker rm $n > /dev/null 2>&1 || true
      docker run -i --name $n $n
      mkdir -p ../../packages/$d/
      docker cp $n:/output/. ../../packages/$d/
      docker rm $n > /dev/null
      cd ../
    fi
  done
cd ../../
