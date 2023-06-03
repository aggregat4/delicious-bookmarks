#!/bin/bash
echo Start
PASS=`./gobookmarks -passwordtohash 'bar'`
echo "Hashed Password: " "$PASS"
./gobookmarks -initdb-username foo -initdb-pass "$PASS"
