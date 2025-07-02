#!/bin/bash

set -e

echo "Running tests with FTS5 support"
go test -tags=fts5 ./...

#echo "Checking race conditions"
#go test -race -tags=fts5 ./...

#echo "Creating coverage report"
#go test -coverprofile=coverage.out -tags=fts5 ./...
#go tool cover -func coverage.out
#go tool cover -html=coverage.out -o coverage.html

echo "Tests passed"
