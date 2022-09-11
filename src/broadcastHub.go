package main

import (
	"context"
	"encoding/json"
	"github.com/sirupsen/logrus"
	"main/set"
	"runtime/debug"
	"sort"
	"strconv"
	"time"
)

func newBroadcastHub(sid string, registerHub *RegisterHub) *BroadcastHub {
	return &BroadcastHub{
		incoming:           make(chan HubMessage, 2048),
		register:           make(chan *Client, 2048),
		unregister:         make(chan *Client, 256),
		sid:                sid,
		positions:          make(chan *Client, 2048),
		stateRequests:      make(chan bool, 256),
		registerHub:        registerHub,
		speakerLocation:    RoomLocation{},
		quietLocation:      RoomLocation{},
		screenLocationX:    0.0,
		screenLocationY:    0.0,
		quit:               make(chan bool),
		publisherRadarSize: 1740.0,
		bubleRadius:        240.0,
		width:              9000,
		height:             19488,
		mainSpawnX1:        500.0,
		mainSpawnX2:        1500.0,
		mainSpawnY1:        500.0,
		mainSpawnY2:        1500.0,
		withSpeakers:       false,
		OwnerID:            "0",
		handsAllowed:       true,
	}
}

func (h *BroadcastHub) runBroadcastHub(ctx context.Context, clientRequestRegister ClientRequestRegister) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	h.updatePublisherRadarSize(ctx, clientRequestRegister)
	go h.checkStateRequests(ctx)
	go h.checkPositions(ctx)

	var stopCounter int = 0

	ticker := time.NewTicker(5 * time.Second)

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("crash: %v", r)
			log.Errorf("stacktrace from panic: \n%v", string(debug.Stack()))
			ticker.Stop()
			close(h.positions)
			close(h.stateRequests)
			log.Infof("stop")
			select {
			case h.registerHub.unregisterHub <- h.sid:
			default:
				log.Info("unregisterHub closed")
			}
			close(h.quit)
		}
	}()

	defer func() {
		roomsGauge.Dec()
	}()
	roomsGauge.Inc()

	defer func() {
		ticker.Stop()
		close(h.positions)
		close(h.stateRequests)
		log.Info("stop")

		EmojiStatMap := make(map[string]map[string]int)
		h.EmojiStat.Range(func(k interface{}, v interface{}) bool {
			EmojiStatMap[k.(string)] = v.(map[string]int)
			return true
		})

		log.Infof("EmojiStat: %v", EmojiStatMap)
		select {
		case h.registerHub.unregisterHub <- h.sid:
		default:
			log.Info("unregisterHub closed")
		}
		close(h.quit)
	}()
LOOP:
	for {
		select {
		case client := <-h.register:
			h.regClient(ctx, client)
		case client := <-h.unregister:
			ctx := setClient(ctx, client)
			log := logrus.WithContext(ctx)

			log.Infof("got req client=%v", client)
			if !client.Expired {
				client.Expired = true
				select {
				case client.registerHub.unregisterClient <- client:
				default:
					log.Infof("registerHub unregister closed")
				}
				if _, ok := h.nowSpeaking.Load(client.ID); ok {
					h.nowSpeaking.Delete(client.ID)
				}
				if _, ok := h.Hands.Load(client.ID); ok {
					h.Hands.Delete(client.ID)
				}

				go h.broadcastState(ctx)
				go h.delayUnregister(ctx, client, 30)
			}
		case hubMessage := <-h.incoming:
			ctx := setClient(ctx, hubMessage.Client)
			log := logrus.WithContext(ctx)

			var clientRequest ClientRequest
			reqErr := json.Unmarshal(hubMessage.Message, &clientRequest)
			if reqErr != nil {
				log.WithError(reqErr).Warn("cannot unmarshal ClientRequest")
				hubMessage.Client.sendError(ctx, ErrorResponse{
					badRequestCode,
					"Incorrect request JSON " + reqErr.Error(),
				})
			} else {
				switch clientRequest.Type {
				case "positions":
					go h.sendPositions(ctx, hubMessage)
				case "participant":
					go h.sendParticipant(ctx, clientRequest, hubMessage)
				case "getPath":
					go h.broadcastReceivedPath(ctx, clientRequest, hubMessage)
				case "audioLevel":
					go h.broadcastReceivedAudioLevel(ctx, clientRequest, hubMessage)
				case "broadcast":
					go h.broadcastFromUser(ctx, clientRequest, hubMessage)
				case "userState":
					go h.updateUserState(ctx, clientRequest, hubMessage)
				case "moveToStage":
					go h.moveToStage(ctx, clientRequest, hubMessage)
				case "moveFromStage":
					go h.moveFromStage(ctx, clientRequest, hubMessage)
				case "handUp":
					go h.handUp(ctx, clientRequest, hubMessage)
				case "handDown":
					go h.handDown(ctx, clientRequest, hubMessage)
				case "callToStage":
					go h.callToStage(ctx, clientRequest, hubMessage)
				case "declineCallToStage":
					go h.declineCallToStage(ctx, clientRequest, hubMessage)
				case "addAdmin":
					go h.addAdmin(ctx, clientRequest, hubMessage)
				case "removeAdmin":
					go h.removeAdmin(ctx, clientRequest, hubMessage)
				case "updateProfile":
					go h.updateProfile(ctx, hubMessage)
				case "mute":
					go h.muteUser(ctx, clientRequest, hubMessage)
				case "becomeAbsoluteSpeaker":
					go h.becomeAbsoluteSpeaker(ctx, clientRequest, hubMessage)
				case "setHandsAllowed":
					go h.setHandsAllowed(ctx, clientRequest, hubMessage)
				default:
					log.Warnf("incorrect value type: %v", clientRequest.Type)
					hubMessage.Client.sendError(ctx, ErrorResponse{
						unsupportedTypeCode,
						"Incorrect value Type: " + clientRequest.Type,
					})
				}

			}
		case <-ticker.C:
			clientLength := 0

			h.clients.Range(func(ikey, _ interface{}) bool {
				//ignore abnormal clients
				if ikey.(*Client).Type != ClientTypeNormal {
					return true
				}
				clientLength++
				return false
			})

			if clientLength == 0 {
				if stopCounter > 1 {
					break LOOP
				}
				stopCounter++
			} else {
				stopCounter = 0
			}
		}
	}
}

func (h *BroadcastHub) regClient(ctx context.Context, client *Client) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, client)
	log := logrus.WithContext(ctx)

	log.Infof("new pending connection: %v", client)

	defer func() {
		if r := recover(); r != nil {
			log.Infof("client radar closed: %v", r)
		}
	}()

	if _, ok := h.AdminsIDs.Load(client.ID); ok {
		client.isAdmin = true
	}

	if client.ID == h.OwnerID {
		client.isOwner = true
	}

	prevclient := h.findClient(client.ID)

	if prevclient != nil {
		select {
		case prevclient.registerHub.unregisterClient <- prevclient:
		default:
			log.Infof("registerHub unregister closed")
		}
		h.clients.Delete(prevclient)
		h.fixLastRadarRules(prevclient.ID)
		client.PrevPositionX = prevclient.PrevPositionX + 1.0
		client.PrevPositionY = prevclient.PrevPositionY + 1.0
		client.LastPositionX = prevclient.LastPositionX + 1.0
		client.LastPositionY = prevclient.LastPositionY + 1.0
		client.isInSpeakerLocation = prevclient.isInSpeakerLocation
		client.isInQuietLocation = prevclient.isInQuietLocation
		client.Inited = prevclient.Inited
		client.Jitsi = prevclient.Jitsi
		client.Video = prevclient.Video
		client.Audio = prevclient.Audio
		client.PhoneCall = prevclient.PhoneCall
		client.Mode = prevclient.Mode
		client.PopupTime = prevclient.PopupTime
		client.InvitedToStage = prevclient.InvitedToStage
		client.isSpecialGuest = prevclient.isSpecialGuest
		client.Restoring = true
	} else {
		if h.withSpeakers == true {
			if _, ok := h.SpeakerIDs.Load(client.ID); !ok {
				client.Mode = "popup"
			}
		}
		client.isSpecialGuest = h.SpecialGuestIDs.Contains(client.ID)
	}
	client.checkLocations(ctx, h)
	h.clients.Store(client, true)
	log.Infof("new connection")
	if client.Inited {
		go h.broadcastState(ctx)
		go h.broadcastConnectionState(ctx, client.ID, "connect", client.Mode)
	}

	select {
	case client.radar <- RadarRequest{
		Participants: client.LastRadarRules,
		RadarVolume:  client.LastRadarVolume,
		Delay:        1,
		IsSubscriber: true,
		ScreenVolume: h.getScreenVolume(client),
	}:
	default:
		log.Warn("client radar full")
	}
}

func (h *BroadcastHub) fixLastRadarRules(prevClientID string) {
	h.clients.Range(func(ikey, _ interface{}) bool {
		client, _ := ikey.(*Client)
		if len(client.LastRadarRules) > 0 {
			newRadar := []string{}
			newVol := []radarVolumeStruct{}
			for _, x := range client.LastRadarRules {
				if x != prevClientID {
					newRadar = append(newRadar, x)
				}
			}
			client.LastRadarRules = newRadar
			for _, x := range client.LastRadarVolume {
				if x.ID != prevClientID {
					newVol = append(newVol, x)
				}
			}
			client.LastRadarVolume = newVol
		}
		return true
	})
}

func (h *BroadcastHub) delayUnregister(ctx context.Context, client *Client, delay int) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, client)
	log := logrus.WithContext(ctx)

	log.Info("got req")

	if client.NormalClosure == false {
		go h.broadcastConnectionState(ctx, client.ID, "reconnecting", client.Mode)
		time.Sleep(time.Duration(delay) * time.Second)
	}

	log.Info("unregister connection")
	h.clients.Delete(client)

	newclient := h.findClient(client.ID)

	if newclient == nil {
		go h.broadcastConnectionState(ctx, client.ID, "disconnect", client.Mode)

		h.AbsoluteSpeakerMu.Lock()
		if h.AbsoluteSpeakerID == client.ID {
			h.AbsoluteSpeakerID = ""
			h.broadcastAbsoluteSpeaker(ctx, client, client)
		}
		h.AbsoluteSpeakerMu.Unlock()
	}
}

func (h *BroadcastHub) broadcastClientResponse(ctx context.Context, response []byte, responseType string) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	clientResponse := &ClientResponse{Type: responseType, Payload: response}
	clientResponseJSON, respErr := json.Marshal(clientResponse)
	if respErr != nil {
		log.WithError(respErr).Error("cannot marshal ClientResponse")
	} else {
		go h.broadcastClientResponseJSON(ctx, clientResponseJSON)
	}
}

func (h *BroadcastHub) broadcastClientResponseJSON(ctx context.Context, response []byte) {
	h.clients.Range(func(ikey, _ interface{}) bool {
		client, _ := ikey.(*Client)
		go client.sendResponse(ctx, response)
		return true
	})
}

func (h *BroadcastHub) broadcastConnectionState(ctx context.Context, fromID string, state string, mode string) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	clientResponseConnectionState := &ClientResponseConnectionState{
		ID:    fromID,
		State: state,
		Mode:  mode,
	}

	client := h.findClient(fromID)

	if client != nil {
		shortClient := ShortParticipant{
			ID:      client.ID,
			Name:    client.Name,
			Surname: client.Surname,
			Avatar:  client.AvatarSrc,
			IsAdmin: client.isAdmin,
			IsOwner: client.isOwner,
			Badges:  client.Badges,
		}
		clientResponseConnectionState.User = shortClient
	}
	clientResponseConnectionStateJSON, respPayloadErr := json.Marshal(clientResponseConnectionState)
	if respPayloadErr != nil {
		log.WithError(respPayloadErr).Error("cannot marshal ClientResponseConnectionState")
	} else {
		go h.broadcastClientResponse(ctx, clientResponseConnectionStateJSON, "connectionState")
	}
}

func (h *BroadcastHub) broadcastFromUser(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	clientResponse := &ClientBroadcastResponse{Type: "broadcasted", From: hubMessage.Client.ID, Payload: clientRequest.Payload}
	clientResponseJSON, respErr := json.Marshal(clientResponse)
	if respErr != nil {
		log.WithError(respErr).Error("cannot marshal ClientBroadcastResponse")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respErr.Error(),
		})
		return
	}

	log.Infof("got message for broadcast: %v", string(clientRequest.Payload))

	var broadcastRequest BroadcastRequest
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &broadcastRequest)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal BroadcastRequest")
	} else {
		if broadcastRequest.Type == "nonverbal" && broadcastRequest.Message != "none" {
			if iprev, ok := h.EmojiStat.Load(hubMessage.Client.ID); ok {
				prev, _ := iprev.(map[string]int)
				prev[broadcastRequest.Message]++
				h.EmojiStat.Store(hubMessage.Client.ID, prev)
			} else {
				m := make(map[string]int)
				m[broadcastRequest.Message]++
				h.EmojiStat.Store(hubMessage.Client.ID, m)
			}
			go h.getAPIClient().sendEvent(ctx, h.sid, broadcastRequest.Message, hubMessage.Client.ID)
		}
	}

	go h.broadcastClientResponseJSON(ctx, clientResponseJSON)
}

func (h *BroadcastHub) broadcastReceivedAudioLevel(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	if hubMessage.Client.Mode == "popup" || hubMessage.Client.Type != ClientTypeNormal {
		return
	}

	var clientRequestAudioLevel ClientRequestAudioLevel
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestAudioLevel)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestAudioLevel")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	value, verr := clientRequestAudioLevel.Value.Int64()
	if verr != nil {
		log.WithError(verr).Warn("cannot get audio level")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + verr.Error(),
		})
		return
	}

	if iprev, ok := h.nowSpeaking.Load(hubMessage.Client.ID); ok {
		prev, _ := iprev.(nowSpeakingStruct)
		if value > 0 {
			if prev.Volume == value {
				return
			}

			if prev.Volume < 1000 && value < 1000 {
				return
			}

			if value < 1000 && time.Since(prev.TimeStamp).Seconds() < 2 {
				return
			}
		} else {
			if prev.Volume == value {
				return
			}
		}
	}

	h.nowSpeaking.Store(hubMessage.Client.ID, nowSpeakingStruct{
		Volume:    value,
		TimeStamp: time.Now(),
	})

	go h.broadcastState(ctx)
}

func (h *BroadcastHub) broadcastPositions(ctx context.Context, x float64, y float64, hubMessage HubMessage, skipDuration bool) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	log.Infof("broadcast positions: x=%v, y=%v ", x, y)

	hubMessage.Client.PrevPositionX = hubMessage.Client.LastPositionX
	hubMessage.Client.PrevPositionY = hubMessage.Client.LastPositionY

	var computedPoint CollisionPoint
	var pathPoints []Point
	if x == 0.0 && y == 0.0 {
		computedPoint = CollisionPoint{
			X: x,
			Y: y,
		}
		pathPoint := Point{
			Index:    0,
			X:        Float64ToNumber(x),
			Y:        Float64ToNumber(y),
			Duration: Float64ToNumber(0.0),
		}
		pathPoints = append(pathPoints, pathPoint)

	} else {
		computedPoint, pathPoints = GetPoint(hubMessage.Client, h.staticObjects, h.getCircles(hubMessage), h.bubleRadius, h.width, h.height, CollisionPoint{x, y})
		if skipDuration == true {
			for i := range pathPoints {
				pathPoints[i].Duration = Float64ToNumber(0.0)
			}
		}
	}

	hubMessage.Client.LastPositionX = computedPoint.X
	hubMessage.Client.LastPositionY = computedPoint.Y
	hubMessage.Client.checkLocations(ctx, h)
	if hubMessage.Client.Inited == false {
		hubMessage.Client.Inited = true
		go h.broadcastState(ctx)
		go h.broadcastConnectionState(ctx, hubMessage.Client.ID, "connect", hubMessage.Client.Mode)
	}

	h.broadcastPath(
		ctx,
		hubMessage.Client.PrevPositionX,
		hubMessage.Client.PrevPositionY,
		hubMessage.Client.LastPositionX,
		hubMessage.Client.LastPositionY,
		hubMessage,
		pathPoints,
	)

	select {
	case h.positions <- hubMessage.Client:
	default:
		log.Error("positions chan full")
	}
}

func (h *BroadcastHub) getCircles(hubMessage HubMessage) []CollisionCircle {

	var circles []CollisionCircle

	h.clients.Range(func(ikey, _ interface{}) bool {
		client, _ := ikey.(*Client)
		if client.Inited == false {
			return true
		}

		if h.withSpeakers == true {
			if client.Mode == "popup" {
				return true
			}
		}

		if client.ID != hubMessage.Client.ID {
			circle := CollisionCircle{
				ID:             client.ID,
				CollisionPoint: CollisionPoint{X: client.LastPositionX, Y: client.LastPositionY},
			}
			circles = append(circles, circle)
		}
		return true
	})
	return circles
}

func (h *BroadcastHub) broadcastReceivedPath(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestPath ClientRequestPath
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestPath)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestPath")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	nextX, nxerr := clientRequestPath.NextX.Float64()
	if nxerr != nil {
		log.WithError(nxerr).Warn("cannot get NextX")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + nxerr.Error(),
		})
		return
	}

	nextY, nyerr := clientRequestPath.NextY.Float64()
	if nyerr != nil {
		log.WithError(nyerr).Warn("cannot get NextY")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + nyerr.Error(),
		})
		return
	}

	log.Infof("message: %v", string(hubMessage.Message))

	if hubMessage.Client.LastPositionX == 0 && hubMessage.Client.LastPositionY == 0 {
		if h.withSpeakers == true && hubMessage.Client.Mode == "popup" {
			nextX = 0.0
			nextY = 0.0
		} else {
			nextX, nextY = h.getSpawn()
		}
	}

	if hubMessage.Client.Restoring == true {
		hubMessage.Client.Restoring = false
		nextX = hubMessage.Client.LastPositionX
		nextY = hubMessage.Client.LastPositionY
	}

	h.broadcastPositions(ctx, nextX, nextY, hubMessage, false)
}

func (h *BroadcastHub) getSpawn() (nextX float64, nextY float64) {
	return RandFloats(h.mainSpawnX1, h.mainSpawnX2, 1)[0], RandFloats(h.mainSpawnY1, h.mainSpawnY2, 1)[0]
}

func (h *BroadcastHub) broadcastPath(ctx context.Context, prevX float64, prevY float64, nextX float64, nextY float64, hubMessage HubMessage, pathPoints []Point) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	clientResponsePath := &ClientResponsePath{ID: hubMessage.Client.ID, Points: pathPoints}

	clientResponsePathJSON, respPayloadErr := json.Marshal(clientResponsePath)
	if respPayloadErr != nil {
		log.WithError(respPayloadErr).Error("cannot marshal ClientResponsePath: %v", clientResponsePath)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respPayloadErr.Error(),
		})
		return
	}

	go h.broadcastClientResponse(ctx, clientResponsePathJSON, "path")
}

func (h *BroadcastHub) broadcastAbsoluteSpeaker(ctx context.Context, target, source *Client) {
	log := logrus.WithContext(ctx)

	var verb string
	if h.AbsoluteSpeakerID == "" {
		verb = "clear"
	} else {
		verb = "set"
	}

	response, err := createServerNotify("serverAbsoluteSpeakerNotify", verb, target, source)
	if err != nil {
		log.WithError(err).Error("cannot create server notify message")
		source.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + err.Error(),
		})
		return
	}

	h.clients.Range(func(ikey, _ interface{}) bool {
		hubClient, _ := ikey.(*Client)
		go hubClient.sendResponse(ctx, response)
		return true
	})
}

func (h *BroadcastHub) broadcastHandsAllowed(ctx context.Context, whoChanged *Client) {
	log := logrus.WithContext(ctx)

	var verb string
	if h.handsAllowed {
		verb = "allowed"
	} else {
		verb = "banned"
	}

	response, err := createServerNotify("serverHandsAllowedNotify", verb, whoChanged, whoChanged)
	if err != nil {
		log.WithError(err).Error("cannot create server notify message")
		whoChanged.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + err.Error(),
		})
		return
	}

	h.clients.Range(func(ikey, _ interface{}) bool {
		hubClient, _ := ikey.(*Client)
		go hubClient.sendResponse(ctx, response)
		return true
	})
}

func (h *BroadcastHub) sendPositions(ctx context.Context, hubMessage HubMessage) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	clientResponsePositionJSON, respPayloadErr := h.getState(hubMessage.Client, true)
	if respPayloadErr != nil {
		log.WithError(respPayloadErr).Error("cannot get client state")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respPayloadErr.Error(),
		})
		return
	}

	clientResponse := &ClientResponse{Type: "positions", Payload: clientResponsePositionJSON}
	clientResponseJSON, respErr := json.Marshal(clientResponse)
	if respErr != nil {
		log.WithError(respErr).Error("cannot marshal ClientResponse")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respErr.Error(),
		})
		return
	}

	go hubMessage.Client.sendResponse(ctx, clientResponseJSON)
}

func (h *BroadcastHub) moveToStage(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	if h.withSpeakers == false {
		return
	}

	if hubMessage.Client.Type != ClientTypeNormal {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestStage ClientRequestStage
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestStage)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestStage")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	client := h.findClient(clientRequestStage.ID)

	if client == nil {
		log.Warnf("no such client: id=%v", clientRequestStage.ID)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			notFound,
			"No such participant",
		})
		return
	}

	if client.Mode == "room" || client.Type != ClientTypeNormal {
		return
	}

	if hubMessage.Client.isAdmin == false {
		if client.InvitedToStage == false {
			return
		}
		if client.ID != hubMessage.Client.ID {
			return
		}
	}

	client.InvitedToStage = false

	h.Hands.Delete(client.ID)

	hubMessage.Client = client
	nextX, nextY := h.getSpawn()
	client.Mode = "room"
	h.SpeakerIDs.Store(client.ID, true)
	h.broadcastConnectionState(ctx, client.ID, "mode", client.Mode)
	h.broadcastPositions(ctx, nextX, nextY, hubMessage, true)
	go h.broadcastState(ctx)

	go h.getAPIClient().sendEvent(ctx, h.sid, "moveToStage", hubMessage.Client.ID)
}

func (h *BroadcastHub) moveFromStage(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	if h.withSpeakers == false {
		return
	}

	if hubMessage.Client.Type != ClientTypeNormal {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestStage ClientRequestStage
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestStage)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestStage")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	client := h.findClient(clientRequestStage.ID)

	if client == nil {
		log.Warnf("no such client: id=%v", clientRequestStage.ID)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			notFound,
			"No such participant",
		})
		return
	}

	if client.Type != ClientTypeNormal {
		return
	}

	if hubMessage.Client.isAdmin == false {
		if hubMessage.Client.ID != client.ID {
			return
		}
	}

	client.InvitedToStage = false

	hubMessage.Client = client
	client.Mode = "popup"
	h.SpeakerIDs.Delete(client.ID)
	client.PopupTime = time.Now().UnixNano()

	h.AbsoluteSpeakerMu.Lock()
	if h.AbsoluteSpeakerID == client.ID {
		h.AbsoluteSpeakerID = ""
		h.broadcastAbsoluteSpeaker(ctx, client, client)
	}
	h.AbsoluteSpeakerMu.Unlock()

	h.broadcastConnectionState(ctx, client.ID, "mode", client.Mode)
	h.broadcastPositions(ctx, 0.0, 0.0, hubMessage, true)
	go h.broadcastState(ctx)
}

func (h *BroadcastHub) handUp(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	if !h.withSpeakers || !h.handsAllowed {
		return
	}

	if hubMessage.Client.Type != ClientTypeNormal {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestHand ClientRequestHand
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestHand)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestHand")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	client := h.findClient(clientRequestHand.ID)

	if client == nil {
		log.Warnf("no such client: id=%v", clientRequestHand.ID)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			notFound,
			"No such participant",
		})
		return
	}

	if client.Type != ClientTypeNormal {
		return
	}

	if _, ok := h.Hands.Load(client.ID); ok {
		return
	}

	h.Hands.Store(client.ID, true)
	client.CallToStageNotificationTime.Range(func(key, value interface{}) bool {
		client.CallToStageNotificationTime.Delete(key)
		return true
	})

	go h.broadcastState(ctx)

	go h.getAPIClient().sendEvent(ctx, h.sid, "handUp", client.ID)

	if !client.HandUpNotificationTime.IsZero() && time.Now().Sub(client.HandUpNotificationTime) < time.Minute {
		return
	}

	h.clients.Range(func(key, _ interface{}) bool {
		hubClient, _ := key.(*Client)
		if !hubClient.Inited || !hubClient.isAdmin {
			return true
		}
		go hubClient.sendServerNotify(ctx, "serverHandNotify", "request", client, hubMessage.Client)
		return true
	})
	client.HandUpNotificationTime = time.Now()
}

func (h *BroadcastHub) handDown(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	if h.withSpeakers == false {
		return
	}

	if hubMessage.Client.Type != ClientTypeNormal {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestHand ClientRequestHand
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestHand)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestHand")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	client := h.findClient(clientRequestHand.ID)

	if client == nil {
		log.Warnf("no such client: id=%v", clientRequestHand.ID)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			notFound,
			"No such participant",
		})
		return
	}

	if client.Type != ClientTypeNormal {
		return
	}

	if _, ok := h.Hands.Load(client.ID); !ok {
		return
	}

	h.Hands.Delete(client.ID)
	go h.broadcastState(ctx)
	if clientRequestHand.Type == "admin" {
		go client.sendServerNotify(ctx, "serverHandNotify", "reject", client, hubMessage.Client)
	}
}

func (h *BroadcastHub) setHandsAllowed(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	if !h.withSpeakers || !(hubMessage.Client.isAdmin || hubMessage.Client.isOwner) {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestSetHandsAllowed ClientRequestSetHandsAllowed
	if err := json.Unmarshal(clientRequest.Payload, &clientRequestSetHandsAllowed); err != nil {
		log.WithError(err).Warn("cannot unmarshal ClientRequestSetHandsAllowed")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + err.Error(),
		})
		return
	}

	h.handsAllowedMu.Lock()
	defer h.handsAllowedMu.Unlock()

	if h.handsAllowed != clientRequestSetHandsAllowed.Value {
		h.handsAllowed = clientRequestSetHandsAllowed.Value

		if !clientRequestSetHandsAllowed.Value {
			h.Hands.Range(func(key, _ interface{}) bool {
				h.Hands.Delete(key)
				return true
			})
		}

		h.broadcastHandsAllowed(ctx, hubMessage.Client)
		go h.broadcastState(ctx)
	}

}

func (h *BroadcastHub) callToStage(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	if h.withSpeakers == false {
		return
	}

	if hubMessage.Client.isAdmin == false || hubMessage.Client.Type != ClientTypeNormal {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestStage ClientRequestStage
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestStage)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestStage")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	client := h.findClient(clientRequestStage.ID)

	if client == nil {
		log.Warnf("no such client: id=%v", clientRequestStage.ID)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			notFound,
			"No such participant",
		})
		return
	}

	if client.Mode == "room" {
		return
	}

	client.InvitedToStage = true

	h.Hands.Delete(client.ID)
	go h.broadcastState(ctx)

	client.HandUpNotificationTime = time.Time{}

	if notificationTime, ok := client.CallToStageNotificationTime.Load(hubMessage.Client.ID); ok {
		if time.Now().Sub(notificationTime.(time.Time)) < time.Minute {
			return
		}
	}

	go client.sendServerNotify(ctx, "serverHandNotify", "invite", client, hubMessage.Client)

	client.CallToStageNotificationTime.Store(hubMessage.Client.ID, time.Now())
}

func (h *BroadcastHub) declineCallToStage(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	if h.withSpeakers == false {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestDeclineStage ClientRequestDeclineStage
	if err := json.Unmarshal(clientRequest.Payload, &clientRequestDeclineStage); err != nil {
		log.WithError(err).Warn("cannot unmarshal ClientRequestDeclineStage")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + err.Error(),
		})
		return
	}

	h.Hands.Delete(hubMessage.Client.ID)
	hubMessage.Client.InvitedToStage = false

	go h.broadcastState(ctx)

	inviterClient := h.findClient(clientRequestDeclineStage.InviterID)

	if inviterClient == nil {
		log.Infof("no such inviter(clinetId=%v)", clientRequestDeclineStage.InviterID)
		return
	}

	if !inviterClient.isAdmin {
		log.Infof("the inviter (clientId=%v) is not admin", clientRequestDeclineStage.InviterID)
	}

	go inviterClient.sendServerNotify(ctx, "serverHandNotify", "declineInvite", hubMessage.Client, hubMessage.Client)
}

func (h *BroadcastHub) addAdmin(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {

	if hubMessage.Client.isAdmin == false {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestStage ClientRequestStage
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestStage)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestStage")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	client := h.findClient(clientRequestStage.ID)

	if client == nil {
		log.Warnf("no such client: id=%v", clientRequestStage.ID)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			notFound,
			"No such participant",
		})
		return
	}

	if client.isAdmin == true {
		return
	}

	h.AdminsIDs.Store(client.ID, true)
	client.isAdmin = true
	h.Hands.Delete(client.ID)

	go h.broadcastState(ctx)

	go client.sendServerNotify(ctx, "serverAdminNotify", "add", client, hubMessage.Client)
	go h.getAPIClient().promoteUser(ctx, h.sid, client.ID)
}

func (h *BroadcastHub) removeAdmin(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {

	if hubMessage.Client.isAdmin == false {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestStage ClientRequestStage
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestStage)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestStage")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	client := h.findClient(clientRequestStage.ID)

	if client == nil {
		log.Warnf("no such client: id=%v", clientRequestStage.ID)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			notFound,
			"No such participant",
		})
		return
	}

	if client.isOwner == true {
		return
	}

	if client.isAdmin == false {
		return
	}

	h.AdminsIDs.Delete(client.ID)
	client.isAdmin = false

	go h.broadcastState(ctx)
	go h.getAPIClient().demoteUser(ctx, h.sid, client.ID)
}

func (h *BroadcastHub) muteUser(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {

	if hubMessage.Client.isAdmin == false {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestMute ClientRequestMute
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestMute)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestMute")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	client := h.findClient(clientRequestMute.ID)

	if client == nil {
		log.Warnf("no such client: id=%v", clientRequestMute.ID)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			notFound,
			"No such participant",
		})
		return
	}

	clientResponseMute := &ClientResponseMute{
		ID:          client.ID,
		Type:        clientRequestMute.Type,
		FromName:    hubMessage.Client.Name,
		FromSurname: hubMessage.Client.Surname,
	}

	clientResponseMuteJSON, respPayloadErr := json.Marshal(clientResponseMute)
	if respPayloadErr != nil {
		log.WithError(respPayloadErr).Error("cannot marshal ClientResponseMute")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respPayloadErr.Error(),
		})
		return
	}

	clientResponse := &ClientResponse{Type: "muteRequest", Payload: clientResponseMuteJSON}
	clientResponseJSON, respErr := json.Marshal(clientResponse)
	if respErr != nil {
		log.WithError(respErr).Error("cannot marshal ClientResponse")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respErr.Error(),
		})
		return
	}

	go client.sendResponse(ctx, clientResponseJSON)
}

func (h *BroadcastHub) updateProfile(ctx context.Context, hubMessage HubMessage) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	apiCurrentResponse, err := h.getAPIClient().getUserID(ctx, hubMessage.Client.AccessToken)
	if err != nil {
		log.WithError(err).Warn("cannot get user id")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			accessTokenInvalidCode,
			"Cannot authorize access token",
		})
		return
	}

	if apiCurrentResponse.Response.ID < 1 {
		log.Warn("user id is negative")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			accessTokenInvalidCode,
			"Cannot authorize access token",
		})
		return
	}

	hubMessage.Client.Name = apiCurrentResponse.Response.Name
	hubMessage.Client.Surname = apiCurrentResponse.Response.Surname
	hubMessage.Client.AvatarSrc = apiCurrentResponse.Response.AvatarSrc
}

func (h *BroadcastHub) becomeAbsoluteSpeaker(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	if !hubMessage.Client.isAdmin && !hubMessage.Client.isOwner {
		return
	}

	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestBecomeAbsoluteSpeaker ClientRequestBecomeAbsoluteSpeaker
	err := json.Unmarshal(clientRequest.Payload, &clientRequestBecomeAbsoluteSpeaker)
	if err != nil {
		log.WithError(err).Warn("cannot unmarshal ClientRequestBecomeAbsoluteSpeaker")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + err.Error(),
		})
		return
	}

	h.AbsoluteSpeakerMu.Lock()
	defer h.AbsoluteSpeakerMu.Unlock()

	if clientRequestBecomeAbsoluteSpeaker.State {
		if h.AbsoluteSpeakerID == "" {
			h.AbsoluteSpeakerID = hubMessage.Client.ID
			h.broadcastAbsoluteSpeaker(ctx, hubMessage.Client, hubMessage.Client)
		} else {
			log.Warnf("clientId = %v is absolute speaker now", h.AbsoluteSpeakerID)
		}
	} else {
		if h.AbsoluteSpeakerID == hubMessage.Client.ID {
			h.AbsoluteSpeakerID = ""
			h.broadcastAbsoluteSpeaker(ctx, hubMessage.Client, hubMessage.Client)
		} else {
			log.Warnf("only clientId = %v can unset absolute speaker", h.AbsoluteSpeakerID)
		}
	}
	go h.broadcastState(ctx)
}

func (h *BroadcastHub) updateUserState(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestUserState ClientRequestUserState
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestUserState)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestUserState")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	notify := false

	if hubMessage.Client.Jitsi != clientRequestUserState.Jitsi {
		notify = true
	}
	hubMessage.Client.Jitsi = clientRequestUserState.Jitsi

	onEvent := func(event string) {
		notify = true
		go h.getAPIClient().sendEvent(ctx, h.sid, event, hubMessage.Client.ID)
	}
	setAndMakeEvent(&hubMessage.Client.Video, clientRequestUserState.Video, "videoOn", "videoOff", onEvent)
	setAndMakeEvent(&hubMessage.Client.Audio, clientRequestUserState.Audio, "audioOn", "audioOff", onEvent)

	if hubMessage.Client.PhoneCall != clientRequestUserState.PhoneCall {
		notify = true
	}
	hubMessage.Client.PhoneCall = clientRequestUserState.PhoneCall

	if notify == true {
		go h.broadcastState(ctx)
	}
}

func (h *BroadcastHub) getState(currentClient *Client, withPopup bool) (json.RawMessage, error) {
	txn := h.newRelic.StartTransaction("getState")
	defer txn.End()

	var popupParticipants []*Client
	var current Participant
	room := make(map[string]Participant)

	onlineAdmins := []string{}

	popupArray := []ShortParticipant{}
	listenersCount := 0

	h.clients.Range(func(ikey, _ interface{}) bool {
		client, _ := ikey.(*Client)
		if client.Inited == false {
			return true
		}
		if client.isAdmin == true {
			onlineAdmins = append(onlineAdmins, client.ID)
		}
		if client.ID == currentClient.ID {
			current = h.getParticipant(client, currentClient)
		}
		if h.withSpeakers == true && client.Mode == "popup" {
			popupParticipants = append(popupParticipants, client)
		} else {
			room[client.ID] = h.getParticipant(client, currentClient)
		}
		return true
	})

	sort.SliceStable(popupParticipants, func(i, j int) bool {
		return popupParticipants[i].PopupTime < popupParticipants[j].PopupTime
	})

	if withPopup == true {
		for _, popupParticipant := range popupParticipants {
			listenersCount++
			if popupParticipant.Type != ClientTypeNormal {
				continue
			}
			shortClient := ShortParticipant{
				ID:             popupParticipant.ID,
				Name:           popupParticipant.Name,
				Surname:        popupParticipant.Surname,
				Avatar:         popupParticipant.AvatarSrc,
				IsAdmin:        popupParticipant.isAdmin,
				IsOwner:        popupParticipant.isOwner,
				IsSpecialGuest: popupParticipant.isSpecialGuest,
				Badges:         popupParticipant.Badges,
			}
			popupArray = append(popupArray, shortClient)
		}
	}

	hands := []string{}
	h.Hands.Range(func(ikey, _ interface{}) bool {
		hand, _ := ikey.(string)
		hands = append(hands, hand)
		return true
	})

	admins := []string{}
	h.AdminsIDs.Range(func(ikey, _ interface{}) bool {
		admin, _ := ikey.(string)
		admins = append(admins, admin)
		return true
	})

	sort.Strings(hands)
	sort.Strings(admins)
	sort.Strings(onlineAdmins)

	response5 := &ClientResponseState5{
		Room:                   room,
		Popup:                  popupArray,
		ListenersCount:         listenersCount,
		Current:                current,
		Admins:                 admins,
		Hands:                  hands,
		HandsAllowed:           h.handsAllowed,
		OwnerID:                h.OwnerID,
		OnlineAdmins:           onlineAdmins,
		AbsoluteSpeakerPresent: h.AbsoluteSpeakerID != "",
	}
	return json.Marshal(response5)

}

func (h *BroadcastHub) isInSpeakerLocation(client *Client) bool {
	return client.Inited && client.Mode == "room" &&
		h.speakerLocation != nil &&
		h.speakerLocation.isInside(client.LastPositionX, client.LastPositionY)
}

func (h *BroadcastHub) isInQuietLocation(client *Client) bool {
	return client.Inited && client.Mode == "room" &&
		h.quietLocation != nil &&
		h.quietLocation.isInside(client.LastPositionX, client.LastPositionY)
}

func (h *BroadcastHub) getParticipant(client *Client, currentClient *Client) Participant {

	var audioLevel int64 = 0
	if ival, ok := h.nowSpeaking.Load(client.ID); ok {
		val, _ := ival.(nowSpeakingStruct)
		if val.Volume > 1000 {
			audioLevel = 30
		}
	}

	inRadar := false

	for _, x := range currentClient.LastRadarRules {
		if x == client.ID {
			inRadar = true
			break
		}
	}

	volume := Float64ToNumber(0.0)
	for _, x := range currentClient.LastRadarVolume {
		if x.ID == client.ID {
			volume = x.Volume
			break
		}
	}

	var participant Participant

	x := Float64ToNumber(client.LastPositionX)
	y := Float64ToNumber(client.LastPositionY)

	participant.Name = client.Name
	participant.Surname = client.Surname
	participant.Avatar = client.AvatarSrc
	participant.Expired = client.Expired
	participant.Jitsi = client.Jitsi
	participant.Video = client.Video
	participant.Audio = client.Audio
	participant.PhoneCall = client.PhoneCall
	participant.X = x
	participant.Y = y
	participant.IsAbsoluteSpeaker = client.ID == h.AbsoluteSpeakerID
	participant.IsSpeaker = participant.IsAbsoluteSpeaker || client.isInSpeakerLocation
	participant.AudioLevel = audioLevel
	participant.InRadar = inRadar
	participant.Volume = volume
	participant.Mode = client.Mode
	participant.ID = client.ID
	participant.IsAdmin = client.isAdmin
	participant.IsOwner = client.isOwner
	participant.IsSpecialGuest = client.isSpecialGuest
	participant.Badges = client.Badges

	return participant
}

func (h *BroadcastHub) sendParticipant(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	var clientRequestParticipant ClientRequestParticipant
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestParticipant)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestParticipant")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Incorrect JSON request payload: " + reqPayloadErr.Error(),
		})
		return
	}

	client := h.findClient(clientRequestParticipant.ID)

	if client == nil {
		log.Warnf("no such client: id=%v", clientRequestParticipant.ID)
		hubMessage.Client.sendError(ctx, ErrorResponse{
			notFound,
			"No such participant",
		})
		return
	}

	response := make(map[string]Participant)
	response[client.ID] = h.getParticipant(client, hubMessage.Client)

	clientResponseParticipantJSON, respPayloadErr := json.Marshal(response)
	if respPayloadErr != nil {
		log.WithError(respPayloadErr).Error("cannot marshal Participant")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respPayloadErr.Error(),
		})
		return
	}

	clientResponse := &ClientResponse{Type: "participant", Payload: clientResponseParticipantJSON}
	clientResponseJSON, respErr := json.Marshal(clientResponse)
	if respErr != nil {
		log.WithError(respErr).Error("cannot marshal ClientResponse")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respErr.Error(),
		})
		return
	}

	go hubMessage.Client.sendResponse(ctx, clientResponseJSON)
}

func (h *BroadcastHub) checkPositions(ctx context.Context) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	ticker := time.NewTicker(3 * time.Second)
	defer func() {
		ticker.Stop()
	}()
	for {
		select {
		case currentClient, ok := <-h.positions:
			if !ok {
				// The hub closed the channel.
				log.Info("positions channel closed")
				return
			}
			pathDist := GetDistance(currentClient.LastPositionX, currentClient.LastPositionY, currentClient.PrevPositionX, currentClient.PrevPositionY)
			h.sendUpdateSubsciptions(ctx, currentClient.ID, pathDist)
			go h.updateSubsciptions(ctx, currentClient, pathDist, false)
		case <-ticker.C:
			go h.syncPositions(ctx)
		}
	}
}

func (h *BroadcastHub) sendUpdateSubsciptions(ctx context.Context, currentClientID string, pathDist float64) {
	h.clients.Range(func(ikey, _ interface{}) bool {
		client, _ := ikey.(*Client)
		if client.ID != currentClientID {
			go h.updateSubsciptions(ctx, client, pathDist, true)
		}
		return true
	})
}

func (h *BroadcastHub) syncPositions(ctx context.Context) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	defer func() {
		if r := recover(); r != nil {
			log.Warnf("client radar closed: %v", r)
		}
	}()

	var fClient *Client

	h.clients.Range(func(ikey, _ interface{}) bool {
		tmp, _ := ikey.(*Client)
		if tmp.Inited == false {
			return true
		}
		fClient = tmp
		return false
	})

	if fClient != nil {
		select {
		case h.positions <- fClient:
		default:
			log.Error("positions chan full")
		}
	}
}

func (h *BroadcastHub) updateSubsciptions(ctx context.Context, currentClient *Client, pathDist float64, isSubscriber bool) {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, currentClient)
	log := logrus.WithContext(ctx)

	defer func() {
		if r := recover(); r != nil {
			log.Warnf("client chan closed: %v", r)
			if currentClient.broadcastHub != nil {
				select {
				case currentClient.broadcastHub.unregister <- currentClient:
				default:
					log.Info("broadcastHub unregister closed")
				}
			} else {
				select {
				case currentClient.registerHub.unregisterClient <- currentClient:
				default:
					log.Info("registerHub unregister closed")
				}
			}
		}
	}()

	if currentClient.Inited == false {
		return
	}

	currentIDsubscriptions := h.currentIDsubscriptions(ctx, currentClient)

	rules := currentIDsubscriptions.Participants
	sort.Strings(rules)
	equalRules := EqualRules(currentClient.LastRadarRules, rules)

	var radarDelay int = 0
	if h.withSpeakers == false {
		speedDelay := int(pathDist * bubleSpeed)
		if isSubscriber {
			if len(currentClient.LastRadarRules) < len(rules) {
				radarDelay = speedDelay
			} else {
				radarDelay = int(h.publisherRadarSize * bubleSpeed)
			}
		} else {
			radarDelay = speedDelay
		}
	}

	radarVolume := h.getVolume(currentClient, rules, currentIDsubscriptions.Distances)

	if !equalRules {
		select {
		case currentClient.radar <- RadarRequest{
			Participants: rules,
			RadarVolume:  radarVolume,
			Delay:        radarDelay,
			IsSubscriber: isSubscriber,
			ScreenVolume: h.getScreenVolume(currentClient),
		}:
		default:
			log.Error("client radar full")
		}
	} else {
		select {
		case currentClient.radar <- RadarRequest{
			Participants: rules,
			RadarVolume:  radarVolume,
			Delay:        radarDelay,
			IsSubscriber: false,
			ScreenVolume: h.getScreenVolume(currentClient),
		}:
		default:
			log.Error("client radar full")
		}
	}
}

func (h *BroadcastHub) getVolume(currentClient *Client, rules []string, distances map[string]float64) []radarVolumeStruct {
	radarVolume := []radarVolumeStruct{}

	for i := range rules {
		vol := 0.6

		if h.withSpeakers == false {
			if dist, ok := distances[rules[i]]; ok {
				if dist > h.publisherRadarSize {
					vol = 0.6
				}
			}
		}

		volPart := radarVolumeStruct{ID: rules[i], Volume: Float64ToNumber(vol)}
		radarVolume = append(radarVolume, volPart)
	}

	return radarVolume
}

func (h *BroadcastHub) getScreenVolume(currentClient *Client) float64 {
	screenVolume := 0.6
	if currentClient.isInQuietLocation {
		screenVolume = 0
	}
	return screenVolume
}

func (h *BroadcastHub) currentIDsubscriptions(ctx context.Context, currentClient *Client) ClientRequestSubscriptions {
	ctx = setBroadcastHub(ctx, h)
	ctx = setClient(ctx, currentClient)
	log := logrus.WithContext(ctx)

	var subscriberSorting []SubscriberSorting

	distMap := make(map[string]float64)

	h.clients.Range(func(ikey, _ interface{}) bool {
		client, _ := ikey.(*Client)
		if client.Inited == false {
			log.Infof("skip client(%v), cause Inited==false", client.ID)
			return true
		}
		if client.Jitsi == false {
			log.Infof("skip client(%v), cause Jitsi==false", client.ID)
			return true
		}
		if client.ID == currentClient.ID {
			return true
		}

		clientIsAbsoluteSpeaker := client.ID == h.AbsoluteSpeakerID

		var dist float64
		if currentClient.Mode == "room" && client.Mode == "room" {
			dist = GetDistance(currentClient.LastPositionX, currentClient.LastPositionY, client.LastPositionX, client.LastPositionY)
			distMap[client.ID] = dist
		}

		subscribe := false
		switch currentClient.Mode {
		case "popup":
			if /*broadcasting room*/ h.allSpeakers {
				subscribe = client.Mode == "room"
			} else /*networking room*/ {
				subscribe = client.isInSpeakerLocation || clientIsAbsoluteSpeaker
			}
		case "room":
			if /*broadcasting room*/ h.allSpeakers {
				subscribe = client.Mode == "room"
			} else /*networking room*/ {
				if clientIsAbsoluteSpeaker {
					subscribe = true
				} else if currentClient.isInSpeakerLocation {
					subscribe = client.isInSpeakerLocation
				} else if currentClient.isInQuietLocation {
					subscribe = dist <= h.publisherRadarSize
				} else {
					subscribe = client.isInSpeakerLocation || dist <= h.publisherRadarSize
				}
			}
		default:
			panic("not implemented mode = " + currentClient.Mode)
		}

		if subscribe {
			var priority int
			priorityBits := []bool{
				client.Audio,
				client.Video,
				client.isAdmin,
				client.isSpecialGuest,
				client.isOwner,
				clientIsAbsoluteSpeaker,
			}
			for i, o := range priorityBits {
				if o {
					priority |= 1 << i
				}
			}
			subscriberSorting = append(subscriberSorting, SubscriberSorting{ID: client.ID, Priority: priority, Dist: dist})
		}
		return true
	})

	sort.Slice(subscriberSorting, func(i, j int) bool {
		x, y := subscriberSorting[i], subscriberSorting[j]
		if x.Priority == y.Priority {
			return x.Dist < y.Dist
		}
		return x.Priority > y.Priority
	})

	currentIDsubscriptions := ClientRequestSubscriptions{Participants: []string{}, Distances: distMap}

	// find nearest 18 participants because of restrictions on mobile platforms
	for i, s := range subscriberSorting {
		if i > 17 {
			var excludedUsers []string
			for j := i; j < len(subscriberSorting); j++ {
				excludedUsers = append(excludedUsers, subscriberSorting[j].ID)
			}
			log.Infof("excluded participants from subscription %v", excludedUsers)
			break
		}
		currentIDsubscriptions.Participants = append(currentIDsubscriptions.Participants, s.ID)
	}
	log.Infof("participants of subscription %v", currentIDsubscriptions.Participants)

	//todo: seems this is not necessary
	currentIDsubscriptions.Participants = removeDuplicateStringValues(currentIDsubscriptions.Participants)

	return currentIDsubscriptions
}

func (h *BroadcastHub) updatePublisherRadarSize(ctx context.Context, clientRequestRegister ClientRequestRegister) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	var roomResp APIRoomResponse

	tmp, err := h.getAPIClient().getRoomByName(ctx, clientRequestRegister.AccessToken, h.sid, clientRequestRegister.Password)
	if err != nil {
		log.WithError(err).Error("cannot get room by name")
		return
	}
	roomResp = tmp

	if roomResp.Response.Config.WithSpeakers == true {
		h.withSpeakers = true
	}

	for _, adminID := range roomResp.Response.AdminsIDs {
		h.AdminsIDs.Store(strconv.Itoa(adminID), true)
	}
	for _, speakerID := range roomResp.Response.SpeakerIds {
		h.SpeakerIDs.Store(strconv.Itoa(speakerID), true)
	}
	h.SpecialGuestIDs = set.NewStringSetSized(len(roomResp.Response.SpecialGuests))
	for _, specialGuestID := range roomResp.Response.SpecialGuests {
		h.SpecialGuestIDs.Add(strconv.Itoa(specialGuestID))
	}
	h.OwnerID = strconv.Itoa(roomResp.Response.OwnerID)

	radarSize, rserr := roomResp.Response.Config.PublisherRadarSize.Float64()
	if rserr != nil {
		log.WithError(rserr).Error("cannot get Config.PublisherRadarSize")
		return
	}

	bubleSize, bserr := roomResp.Response.Config.VideoBubbleSize.Float64()
	if bserr != nil {
		log.WithError(bserr).Error("cannot get Config.VideoBubbleSize")
		return
	}

	h.bubleRadius = bubleSize / 2
	h.publisherRadarSize = (radarSize / 2) + h.bubleRadius

	width, wserr := roomResp.Response.Config.BackgroundRoom.Width.Float64()
	if wserr != nil {
		log.WithError(wserr).Error("cannot get Config.BackgroundRoom.Width")
		return
	}

	height, hserr := roomResp.Response.Config.BackgroundRoom.Height.Float64()
	if hserr != nil {
		log.WithError(hserr).Error("cannot get Config.BackgroundRoom.Height")
		return
	}

	h.allSpeakers = roomResp.Response.Config.PublisherRadarSize.String() == "1"

	h.width = width
	h.height = height

	for _, object := range roomResp.Response.Config.Objects {
		objectX, objectXerr := object.X.Float64()
		if objectXerr != nil {
			log.WithError(objectXerr).Error("cannot get Config.Objects")
			return
		}

		objectY, objectYerr := object.Y.Float64()
		if objectYerr != nil {
			log.WithError(objectYerr).Error("cannot get object.Y")
			return
		}

		objectWidth, objectWerr := object.Width.Float64()
		if objectWerr != nil {
			log.WithError(objectWerr).Error("cannot get object.Width")
			return
		}

		objectHeight, objectHerr := object.Height.Float64()
		if objectHerr != nil {
			log.WithError(objectHerr).Error("cannot get object.Height")
			return
		}

		if object.Type == "main_spawn" {
			h.mainSpawnX1 = objectX
			h.mainSpawnY1 = objectY
			h.mainSpawnX2 = objectX + objectWidth
			h.mainSpawnY2 = objectY + objectHeight
		}

		if object.Type == "speaker_location" {
			h.speakerLocation = append(h.speakerLocation, &CircleRoomObject{
				X:      objectX + objectWidth/2,
				Y:      objectY + objectHeight/2,
				Radius: objectWidth / 2,
			})
		}

		if object.Type == "quiet_zone" {
			h.quietLocation = append(h.quietLocation, &RectangleRoomObject{
				X:      objectX,
				Y:      objectY,
				Width:  objectWidth,
				Height: objectHeight,
			})
		}

		if object.Type == "share_screen" {
			h.screenLocationX = objectX + objectWidth/2
			h.screenLocationY = objectY + objectHeight/2
		}

		if object.Type == "static_object" {
			sObj := CircleRoomObject{
				X:      objectX + objectWidth/2,
				Y:      objectY + objectWidth/2,
				Radius: objectWidth / 2,
			}
			h.staticObjects = append(h.staticObjects, sObj)
		}
	}

	log.Infof("final %v", h)
}

func (h *BroadcastHub) getAPIClient() *APIClient {
	return &APIClient{newRelic: h.newRelic}
}

func (h *BroadcastHub) broadcastState(ctx context.Context) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	select {
	case h.stateRequests <- true:
	default:
		log.Error("stateRequests chan full")
	}
}

func (h *BroadcastHub) broadcastStateSender(ctx context.Context) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	speakers := make([]string, 0) //for json marshaling to empty array
	h.clients.Range(func(ikey, _ interface{}) bool {
		client, _ := ikey.(*Client)
		clientResponsePositionJSON, respPayloadErr := h.getState(client, true)
		if respPayloadErr != nil {
			log.WithError(respPayloadErr).Error("cannot get state")
			return true
		}
		clientResponse := &ClientResponse{Type: "state", Payload: clientResponsePositionJSON}
		clientResponseJSON, respErr := json.Marshal(clientResponse)
		if respErr != nil {
			log.WithError(respErr).Error("cannot marshal ClientResponse")
			return true
		}
		go client.sendResponse(ctx, clientResponseJSON)
		if h.allSpeakers && client.Mode == "room" || client.isInSpeakerLocation {
			speakers = append(speakers, client.ID)
		}
		return true
	})
	h.registerHub.events <- &StateEvent{
		Sid:      h.sid,
		Speakers: speakers,
	}
}

func (h *BroadcastHub) checkStateRequests(ctx context.Context) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	ticker := time.NewTicker(3 * time.Second)
	defer func() {
		ticker.Stop()
	}()
	for {
		select {
		case _, ok := <-h.stateRequests:
			if !ok {
				// The hub closed the channel.
				log.Info("stateRequests channel closed")
				return
			}
			for len(h.stateRequests) > 0 {
				<-h.stateRequests
			}
			h.broadcastStateSender(ctx)
			time.Sleep(1 * time.Second)
		case <-ticker.C:
			h.broadcastStateSender(ctx)
		}
	}
}

func (h *BroadcastHub) findClient(id string) *Client {
	var client *Client

	h.clients.Range(func(ikey, _ interface{}) bool {
		tmp, _ := ikey.(*Client)
		if tmp.Inited == false {
			return true
		}
		if tmp.ID == id {
			client = tmp
			return false
		}
		return true
	})

	return client
}

func (h *BroadcastHub) getSpeakers() []string {
	speakers := []string{}
	if h.withSpeakers == true {
		h.clients.Range(func(ikey, _ interface{}) bool {
			client, _ := ikey.(*Client)
			if client.Inited == false {
				return true
			}
			if client.Mode == "room" {
				speakers = append(speakers, client.ID)
			}
			return true
		})
	}
	return speakers
}

func (h *BroadcastHub) banUser(ctx context.Context, userID string) {
	ctx = setBroadcastHub(ctx, h)
	log := logrus.WithContext(ctx)

	log.Infof("ban user: id=%v", userID)

	h.BanIDs.Store(userID, true)

	var client *Client

	h.clients.Range(func(ikey, _ interface{}) bool {
		tmp, _ := ikey.(*Client)
		if tmp.ID == userID {
			client = tmp
			return false
		}
		return true
	})

	if client != nil {
		client.sendBan(ctx)
	} else {
		log.Warnf("not found: %v", userID)
	}
}
