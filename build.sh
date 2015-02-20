#!/bin/bash

function build() {
    mm_dir=$(dirname `which $0`)
    if [[ $mm_dir == '.' ]] ; then
        mm_dir=`pwd`
    fi
    export GOPATH=$mm_dir

    go get github.com/acierto/go-jira-client
    go get launchpad.net/goyaml

    cp *.go src/github.com/acierto/go-jira-client

    go build
}

build