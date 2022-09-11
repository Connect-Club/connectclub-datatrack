package main

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"strconv"
)

func newRegisterHub() *RegisterHub {
	return &RegisterHub{
		incoming:         make(chan HubMessage, 2048),
		unregisterHub:    make(chan string, 256),
		unregisterClient: make(chan *Client, 256),
		registerClient:   make(chan *Client, 2048),
		events:           make(chan Event, 1024),
	}
}

func (h *RegisterHub) runRegisterHub(ctx context.Context) {
	log := logrus.WithContext(ctx)

	for {
		select {
		case client := <-h.registerClient:
			log.WithContext(setClient(ctx, client)).Infof("registerClient: client=%v", client)
			h.clients.Store(client, true)
			usersGauge.Inc()
		case sid := <-h.unregisterHub:
			log.Infof("unregisterHub: sid=%v", sid)
			if hub, ok := h.hubs.Load(sid); ok {
				h.hubs.Delete(sid)
				hub.(*BroadcastHub).clients.Range(func(client, _ interface{}) bool {
					client.(*Client).sendBan(ctx)
					h.unregisterClient <- client.(*Client)
					return true
				})
			}
		case client := <-h.unregisterClient:
			log.Infof("unregisterClient: clientId=%v", client.ID)
			if _, ok := h.clients.LoadAndDelete(client); ok {
				usersGauge.Dec()
				close(client.send)
			}
		case hubMessage := <-h.incoming:
			ctx := setClient(ctx, hubMessage.Client)
			log := log.WithContext(ctx)
			var clientRequest ClientRequest
			reqErr := json.Unmarshal(hubMessage.Message, &clientRequest)
			if reqErr != nil {
				log.WithError(reqErr).Warn("cannot unmarshal incoming ClientRequest")
				hubMessage.Client.sendError(ctx, ErrorResponse{
					badRequestCode,
					"Cannot parse JSON from message: " + reqErr.Error(),
				})
			} else {
				if clientRequest.Type == "register" {
					go h.registerBroadcastClient(ctx, clientRequest, hubMessage)
				} else {
					log.Warnf("cannot handle client request type=%v", clientRequest.Type)
					hubMessage.Client.sendError(ctx, ErrorResponse{
						unauthorizedCode,
						"Unauthorized Type: " + clientRequest.Type,
					})
				}
			}
		}
	}
}

func (h *RegisterHub) registerBroadcastClient(ctx context.Context, clientRequest ClientRequest, hubMessage HubMessage) {
	ctx = setClient(ctx, hubMessage.Client)
	log := logrus.WithContext(ctx)

	log.Infof("new pending connection: %v", hubMessage.Client)

	txn := h.newRelic.StartTransaction("registerBroadcastClient")
	defer txn.End()

	var clientRequestRegister ClientRequestRegister
	reqPayloadErr := json.Unmarshal(clientRequest.Payload, &clientRequestRegister)
	if reqPayloadErr != nil {
		log.WithError(reqPayloadErr).Warn("cannot unmarshal ClientRequestRegister")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			badRequestCode,
			"Cannot unmarshal client request payload",
		})
		return
	}

	if clientRequestRegister.AccessToken == "" {
		hubMessage.Client.ID = "guest-" + uuid.New().String()
		hubMessage.Client.Type = ClientTypeGuest
	} else if clientRequestRegister.AccessToken == serviceAccessToken {
		hubMessage.Client.ID = "service-" + uuid.New().String()
		hubMessage.Client.Type = ClientTypeService
	} else {
		apiCurrentResponse, err := h.getAPIClient().getUserID(ctx, clientRequestRegister.AccessToken)

		if err != nil {
			log.WithError(err).Error("cannot get user id")
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

		userIDstr := strconv.Itoa(apiCurrentResponse.Response.ID)

		hubMessage.Client.ID = userIDstr
		hubMessage.Client.Type = ClientTypeNormal
		hubMessage.Client.Name = apiCurrentResponse.Response.Name
		hubMessage.Client.Surname = apiCurrentResponse.Response.Surname
		hubMessage.Client.AvatarSrc = apiCurrentResponse.Response.AvatarSrc
		if apiCurrentResponse.Response.Badges == nil {
			hubMessage.Client.Badges = []string{}
		} else {
			hubMessage.Client.Badges = apiCurrentResponse.Response.Badges
		}

		hubMessage.Client.AccessToken = clientRequestRegister.AccessToken
	}
	hubMessage.Client.Version = clientRequestRegister.Version

	log.Infof("new fullfilled connection: %v", hubMessage.Client)

	clientResponseRegister := &ClientResponseRegister{ID: hubMessage.Client.ID, Type: hubMessage.Client.Type}
	clientResponseRegisterJSON, respPayloadErr := json.Marshal(clientResponseRegister)
	if respPayloadErr != nil {
		log.WithError(respPayloadErr).Error("cannot marshal ClientResponseRegister")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respPayloadErr.Error(),
		})
		return
	}

	clientResponse := &ClientResponse{Type: "register", Payload: clientResponseRegisterJSON}
	clientResponseJSON, respErr := json.Marshal(clientResponse)
	if respErr != nil {
		log.WithError(respErr).Error("cannot marshal ClientResponse")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + respErr.Error(),
		})
		return
	}

	log.Infof("find hub connection: %v", hubMessage.Client)

	if ival, ok := h.hubs.Load(clientRequestRegister.SID); ok {
		resHub, _ := ival.(*BroadcastHub)
		hubMessage.Client.broadcastHub = resHub
		if _, ok := resHub.BanIDs.Load(hubMessage.Client.ID); ok {
			go hubMessage.Client.sendBan(ctx)
		}
	} else if hubMessage.Client.Type != ClientTypeNormal {
		log.Error("attempt to create room by an abnormal client")
		hubMessage.Client.sendError(ctx, ErrorResponse{
			accessTokenInvalidCode,
			"An abnormal client cannot create room",
		})
		return
	} else {
		broadcastHub := newBroadcastHub(clientRequestRegister.SID, h)
		broadcastHub.newRelic = h.newRelic
		h.hubs.Store(clientRequestRegister.SID, broadcastHub)
		go broadcastHub.runBroadcastHub(ctx, clientRequestRegister)
		hubMessage.Client.broadcastHub = broadcastHub
		h.events <- &NewBroadcastHubEvent{
			Sid: broadcastHub.sid,
		}
	}
	hubMessage.Client.broadcastHub.register <- hubMessage.Client

	log.Infof("request reg connection: %v", hubMessage.Client)

	go hubMessage.Client.sendResponse(ctx, clientResponseJSON)
}

func (h *RegisterHub) getInfo(names []string) (json.RawMessage, error) {
	response := make(map[string][]string)

	for i := range names {
		if ival, ok := h.hubs.Load(names[i]); ok {
			resHub, _ := ival.(*BroadcastHub)
			response[names[i]] = resHub.getSpeakers()
		}
	}

	return json.Marshal(response)
}

func (h *RegisterHub) getInfo2() (json.RawMessage, error) {
	var rooms []string
	h.hubs.Range(func(client, _ interface{}) bool {
		rooms = append(rooms, client.(*BroadcastHub).sid)
		return true
	})

	response := struct {
		Rooms []string `json:"rooms"`
	}{
		Rooms: rooms,
	}

	return json.Marshal(response)
}

func (h *RegisterHub) banUser(ctx context.Context, roomSid, userID string) {
	log := logrus.WithContext(ctx)

	log.Infof("try: roomSid=%v userID=%v", roomSid, userID)

	if ival, ok := h.hubs.Load(roomSid); ok {
		resHub, _ := ival.(*BroadcastHub)
		resHub.banUser(ctx, userID)
	}
}

func (h *RegisterHub) getAPIClient() *APIClient {
	return &APIClient{newRelic: h.newRelic}
}
