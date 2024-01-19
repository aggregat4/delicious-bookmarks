#!/bin/bash
go build -v -buildvcs=false --tags "fts5" -o bin/gobookmarks cmd/server/main.go
go build -v -buildvcs=false --tags "fts5" -o bin/bookmarkimporter cmd/importer/main.go
