#!/bin/sh

IMAGE=`docker images -q redis`
if [ -z $IMAGE ]; then
    docker pull redis
fi

CONTAINER=`docker ps -a -q -f name="ahocorasick-redis"`
if [ -z $CONTAINER ]; then
    docker run -p 6379:6379 --name ahocorasick-redis -d redis
else
    docker start $CONTAINER
fi