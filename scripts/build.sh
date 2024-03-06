#!/bin/bash
go build -v --tags "fts5" -o bin/bmserver cmd/server/main.go
go build -v --tags "fts5" -o bin/bmimporter cmd/importer/main.go
