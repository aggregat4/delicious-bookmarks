#!/bin/bash
go build -v -buildvcs=false --tags "fts5" -o bin/gobookmarks cmd/server/main.go
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -buildvcs=false --tags "fts5" -o bin/gobookmarks-prod cmd/server/main.go
