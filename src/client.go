package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"time"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 5 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 10 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 3) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 1024
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump(ctx context.Context) {
	ctx = setClient(ctx, c)
	log := logrus.WithContext(ctx)

	defer func() {
		log.Info("close connection")
		if c.broadcastHub != nil {
			select {
			case c.broadcastHub.unregister <- c:
			default:
				log.Info("broadcastHub unregister closed")
			}
		} else {
			select {
			case c.registerHub.unregisterClient <- c:
			default:
				log.Info("registerHub unregisterClient closed")
			}
		}
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			logLevel := logrus.WarnLevel
			if ce, ok := err.(*websocket.CloseError); ok {
				if ce.Code == websocket.CloseNormalClosure {
					c.NormalClosure = true
					logLevel = logrus.InfoLevel
				}
			}
			log.WithError(err).Log(logLevel, "cannot read message from connection")
			break
		}
		log.WithField("connMsg", string(message)).Info("message read from connection")
		messageBoby := bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		hubMessage := &HubMessage{Client: c, Message: messageBoby}

		if c.broadcastHub != nil {
			c.broadcastHub.incoming <- *hubMessage
		} else {
			c.registerHub.incoming <- *hubMessage
		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump(ctx context.Context) {
	ctx = setClient(ctx, c)
	log := logrus.WithContext(ctx)

	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		if c.broadcastHub != nil {
			select {
			case c.broadcastHub.unregister <- c:
			default:
				log.Info("broadcastHub unregister closed")
			}
		} else {
			select {
			case c.registerHub.unregisterClient <- c:
			default:
				log.Info("registerHub unregister closed")
			}
		}
		close(c.radar)
		log.Info("close connection")
		c.conn.WriteMessage(websocket.CloseMessage, []byte{})
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				log.Info("send channel closed")
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				log.WithError(err).Warn("no writer")
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				log.WithError(err).Warn("cannot close writer")
				return
			}
			log.WithField("connMsg", string(message)).Info("message written to connection")
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.WithError(err).Warn("cannot send ping message")
				return
			}
		}
	}
}

func (c *Client) radarSender(ctx context.Context) {
	ctx = setClient(ctx, c)
	log := logrus.WithContext(ctx)

	for {
		select {
		case message, ok := <-c.radar:
			if !ok {
				// The hub closed the channel.
				log.Info("radar channel closed")
				return
			}
			go c.sendRadar(ctx, message.Participants, message.Delay, message.IsSubscriber, message.RadarVolume, message.ScreenVolume)
		}
	}
}

func (c *Client) sendResponse(ctx context.Context, response []byte) {
	ctx = setClient(ctx, c)
	log := logrus.WithContext(ctx)

	txn := c.registerHub.newRelic.StartTransaction("sendResponse")
	defer txn.End()

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("client send closed: response=%v, r=%v, client=%v", string(response), r, c)
			if c.broadcastHub != nil {
				select {
				case c.broadcastHub.unregister <- c:
				default:
					log.Info("broadcastHub unregister closed")
				}
			} else {
				select {
				case c.registerHub.unregisterClient <- c:
				default:
					log.Info("registerHub unregister closed")
				}
			}
		}
	}()

	select {
	case c.send <- response:
	default:
		log.Error("send channel full")
	}
}

func (c *Client) sendError(ctx context.Context, response ErrorResponse) {
	ctx = setClient(ctx, c)

	txn := c.registerHub.newRelic.StartTransaction("sendError")
	defer txn.End()

	errorResponseJSON, err := json.Marshal(response)
	if err != nil {
		c.sendResponse(ctx, []byte("{\"error\": \"\", \"description\": \"\"}"))
		return
	}

	c.sendResponse(ctx, errorResponseJSON)
}

func (c *Client) sendRadar(ctx context.Context, participants []string, delay int, isSubscriber bool, radarVolume []radarVolumeStruct, screenVolume float64) {
	ctx = setClient(ctx, c)
	log := logrus.WithContext(ctx)

	txn := c.registerHub.newRelic.StartTransaction("sendRadar")
	defer txn.End()

	time.Sleep(time.Duration(delay) * time.Millisecond)

	// prevent blinking radar for same rules when publisher moves
	if isSubscriber {
		equalRules := EqualRules(c.LastRadarRules, participants)
		if equalRules {
			isSubscriber = false
		}
	}

	serverRequestRadarVolume := &ServerRequestRadarVolume{
		Participants: participants,
		IsSubscriber: isSubscriber,
		RadarVolume:  radarVolume,
		ScreenVolume: Float64ToNumber(screenVolume),
	}

	serverRequestRadarVolumeJSON, respVPayloadErr := json.Marshal(serverRequestRadarVolume)
	if respVPayloadErr != nil {
		log.WithError(respVPayloadErr).Error("cannot marshal ServerRequestRadarVolume")
		return
	}

	clientResponseVolume := &ClientResponse{Type: "radarVolume", Payload: serverRequestRadarVolumeJSON}
	clientResponseVolumeJSON, respVErr := json.Marshal(clientResponseVolume)
	if respVErr != nil {
		log.WithError(respVErr).Error("cannot marshal ClientResponse")
		return
	}

	c.sendResponse(ctx, clientResponseVolumeJSON)

	c.LastRadarRules = participants
	c.LastRadarVolume = radarVolume
}

func (c *Client) sendBan(ctx context.Context) {
	ctx = setClient(ctx, c)
	log := logrus.WithContext(ctx)
	log.Info("try")

	clientResponseBan := &ClientResponse{Type: "ban"}
	clientResponseBanJSON, respVErr := json.Marshal(clientResponseBan)
	if respVErr != nil {
		log.WithError(respVErr).Error("cannot marshal ClientResponse")
		return
	}

	c.sendResponse(ctx, clientResponseBanJSON)
}

func createServerNotify(notificationType, notificationVerb string, notificationTarget, notificationSource *Client) ([]byte, error) {
	serverNotify := &ServerHandNotify{
		ID:          notificationTarget.ID,
		Type:        notificationVerb,
		FromId:      notificationSource.ID,
		FromName:    notificationSource.Name,
		FromSurname: notificationSource.Surname,
	}

	serverNotifyJSON, err := json.Marshal(serverNotify)
	if err != nil {
		return nil, err
	}

	clientResponse := &ClientResponse{Type: notificationType, Payload: serverNotifyJSON}
	clientResponseJSON, err := json.Marshal(clientResponse)
	if err != nil {
		return nil, err
	}
	return clientResponseJSON, nil
}

func (c *Client) sendServerNotify(ctx context.Context, notificationType, notificationVerb string, notificationTarget, notificationSource *Client) {
	log := logrus.WithContext(ctx)

	response, err := createServerNotify(notificationType, notificationVerb, notificationTarget, notificationSource)
	if err != nil {
		log.WithError(err).Error("cannot create server notify message")
		notificationSource.sendError(ctx, ErrorResponse{
			internalErrorCode,
			"Cannot serialize client response: " + err.Error(),
		})
		return
	}

	c.sendResponse(ctx, response)
}

func setAndMakeEvent(p *bool, v bool, trueEvent, falseEvent string, onEvent func(event string)) {
	if *p != v {
		*p = v
		event := trueEvent
		if !v {
			event = falseEvent
		}
		onEvent(event)
	}
}

func (c *Client) checkLocations(ctx context.Context, h *BroadcastHub) {
	onEvent := func(event string) {
		go h.getAPIClient().sendEvent(ctx, h.sid, event, c.ID)
	}
	setAndMakeEvent(&c.isInSpeakerLocation, h.isInSpeakerLocation(c), "inside-speaker_location", "outside-speaker_location", onEvent)
	setAndMakeEvent(&c.isInQuietLocation, h.isInQuietLocation(c), "inside-quiet_zone", "outside-quiet_zone", onEvent)
}
