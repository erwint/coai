#!/bin/sh
set -eu
mkdir -p bin
go build -trimpath -ldflags "-s -w" -o bin/coai .
