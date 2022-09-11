#!/bin/bash
docker run \
  -e DATATRACK_DEBUG=false \
  -e DATATRACK_NEWRELICNAME="dataTrack" \
  -e DATATRACK_NEWRELICLICENSE="0000000000000000000000000000000000000000" \
  -e DATATRACK_APIURL="https://connect.lol" \
-it -p 8080:8080 -p 8082:8082 --rm data-track
