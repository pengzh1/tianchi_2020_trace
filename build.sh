#!/usr/bin/env bash
hub=
GOOS=linux go build  -o ./tctrace edgeknife/tctrace
version=2.0.1
docker build --build-arg BIN=tctrace -t ${hub}:${version} .
docker push ${hub}:${version}
rm tctrace
