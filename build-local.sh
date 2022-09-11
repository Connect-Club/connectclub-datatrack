#!/bin/bash

export GO111MODULE=auto
PROJECT=$(pwd);
export GOPATH=$PROJECT"/gopath";

GOLINT="../gopath/bin/golint";
cd src;
if [ ! -f "$GOLINT" ]; then
  go get -u golang.org/x/lint/golint;
fi;

gofmt -s -w . && $GOLINT ./... && go vet && go build -o src .;
cd ../;
