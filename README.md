# dataTrack

```shell script
set DATATRACK_NEWRELICLICENSE
./build-local.sh
./run.sh
```

OR

```shell script
./build-docker.sh
./run-docker.sh
```
https://app.cnnct.club/a/key_live_lbUKXoq5Mu4PgpAt7S4v8kceutij138R?room=614d97e007190&pswd=7E4ECpqd33ztcBdk
http://localhost:8080/
#### Reg
##### Register request on start:
```json
{
  "type": "register",
  "payload": {
    "version": 1,
    "sid": "RMe3f5aff566f4442246a538730570342e",
    "accessToken": "OTMzMWRiY2JkM2M0MDJhNGIzN2UzNGJkZmY0MGRmNGZkNDFiZmI0MWQ2MTc4MTdiYzkyZDY5ZGRmNzU5MTM3NQ"
  }
}
```
```json
{
  "type": "register",
  "payload": {
    "version": 5,
    "sid": "614d97e007190",
    "password": "7E4ECpqd33ztcBdk",
    "accessToken": "M2NmM2M3MGE1YzFkZDM0OWQxODM0OGNkOTgwNjI3OTRmM2NiOGMxYWQ0YmNlZDI0ZGM2M2QzY2QwMzFiMzFhNg"
  }
}
```

##### Response on success:
```json
{
  "type": "register",
  "payload": {
    "id": "6"
  }
}
```

#### On connect event broadcasted to all users
```json
{
  "type": "connectionState",
  "payload": {
    "id": "6",
    "state": "connect"
  }
}
```

#### On disconnect event broadcasted to all users
```json
{
  "type": "connectionState",
  "payload": {
    "id": "6",
    "state": "disconnect"
  }
}
```

#### Participants
##### Request all participants:
```json
{
  "type": "positions",
  "payload": {}
}
```
##### Response:
```json
{
  "type": "positions",
  "payload": {
    "1": {
      "name": "Vadim",
      "surname": "Filimonov",
      "avatarSrc": "https://pics.test.connect.lol/:WIDTHx:HEIGHT/8a4cf803-7548-44f6-b850-d2e02fb0dcc2.png",
      "company": "",
      "position": "",
      "about": "",
      "x": 803.2143406263777,
      "y": 495.7728147348174,
      "city":
        {"id":0,"name":""},
      "country":
        {"id":0,"name":""}
    },
    "6": {
      "name": "Vadim",
      "surname": "Filimonov",
      "avatarSrc": "",
      "company": "",
      "position": "",
      "about": "",
      "x": 1429.3587133564615,
      "y": 1280.0518959349092,
      "city":
        {"id":0,"name":""},
      "country":
        {"id":0,"name":""}
    }
  }
}
```
##### Request one participant:
```json
{
  "type": "participant",
  "payload": {
    "id": "6"
  }
}
```
##### Response:
```json
{
  "type": "participant",
  "payload": {
    "6": {
      "name": "Vadim",
      "surname": "Filimonov",
      "avatarSrc": "",
      "company": "",
      "position": "",
      "about": "",
      "x": 1429.3587133564615,
      "y": 1280.0518959349092,
      "city":
        {"id":0,"name":""},
      "country":
        {"id":0,"name":""}
    }
  }
}
```
#### Radar
##### Radar sent from server to current participant with radar rules (isSubscriber - false when changes triggered by current participant, true when changes triggered by another participant):
```json
{
  "type": "radar",
  "payload": {
    "participants": [
      "1"
    ],
    "isSubscriber": false
  }
}
```
#### Path
##### Path - when participant want to move should send a request and move using the respone. spawn - first appearance in a room (do not send on reconnect). move - regular move. portal not in use now (same as spawn)
```json
{
  "type": "getPath",
  "payload": {
    "currentX": 600.1,
    "currentY": 300.2,
    "nextX": 500,
    "nextY": 500,
    "type": "move"
  }
}
```
##### All participants get (duration in ms)
```json
{
  "type": "path",
  "payload": {
    "id": "1",
    "points": [
      {
        "x": 500,
        "y": 500,
        "duration": 1000,
        "index": 0
      }
    ]
  }
}
```

#### AudioLevels
##### AudioLevels - client send every second. on mic disabled should send 0 immediately
```json
{
  "type": "audioLevel",
  "payload": {
    "value": 4066
  }
}
```

##### All participants get info - on change every participant get array of active speakers with value 30. Active speaker - value over 1000.
```json
{
  "type": "audioLevels",
  "payload": {
    "1": 30,
    "2": 30
  }
}
```

##### Broadcast - type for broadcasting from client to all participants any value. used for reactions
```json
{
  "type": "broadcast",
  "payload": {
    "x": 1
  }
}
```
##### All participants get
```json
{
  "type": "broadcasted",
  "from": "2",
  "payload": {
    "x": 1
  }
}
```

##### SpeakerBroadcast - every second broadcasted to all participnats who is speaker
```json
{"type":"speaker","payload":{"id":"0"}}
```
##### VideoBroadcast - every second broadcasted to all participnats from which second to start video if started
```json
{"type":"video","payload":{"from":"0"}}
```

###new
```json
{
  "type": "moveToStage",
  "payload": {
    "id": "40"
  }
}

{
  "type": "moveFromStage",
  "payload": {
    "id": "6"
  }
}

{
  "type": "handUp",
  "payload": {
    "id": "752"
  }
}

{
  "type": "handDown",
  "payload": {
    "id": "754",
    "type": "admin"
  }
}

{
  "type": "handDown",
  "payload": {
    "id": "752",
    "type": ""
  }
}

{
  "type": "callToStage",
  "payload": {
    "id": "752"
  }
}

{
  "type": "addAdmin",
  "payload": {
    "id": "752"
  }
}

{
  "type": "removeAdmin",
  "payload": {
    "id": "752"
  }
}

{
  "type": "serverHandNotify",
  "payload": {
    "id": "752",
    "type": "request",
    "from": "6"
  }
}

{
  "type": "serverHandNotify",
  "payload": {
    "id": "754",
    "type": "reject",
    "from": "6"
  }
}

{
  "type": "serverHandNotify",
  "payload": {
    "id": "754",
    "type": "invite",
    "from": "6"
  }
}

{
  "type": "serverAdminNotify",
  "payload": {
    "id": "754",
    "type": "add",
    "from": "6"
  }
}


```

#### PPROF
##### trace profile
```shell script
curl http://localhost:8082/debug/pprof/trace?seconds=N --output trace.out
```
##### memory profile
```shell script
curl http://localhost:8082/debug/pprof/allocs --output allocs.out
```
##### cpu profile
```shell script
curl http://localhost:8082/debug/pprof/profile?seconds=N --output cpu.out
```
##### heap profile
```shell script
curl http://localhost:8082/debug/pprof/heap --output heap.out
```

#### TSUNG
```shell script
tsung -k -f tsung.xml start
```