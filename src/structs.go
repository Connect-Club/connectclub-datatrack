package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	newrelicV3 "github.com/newrelic/go-agent/v3/newrelic"
	"main/set"
	"sync"
	"time"
)

//Config for app
type Config struct {
	NewRelicName    string
	NewRelicLicense string
	APIURL          string
}

type ClientType int

const (
	ClientTypeNil ClientType = iota
	ClientTypeNormal
	ClientTypeGuest
	ClientTypeService
)

var ClientTypeStrings = [...]string{"Nil", "Normal", "Guest", "Service"}

func (t ClientType) String() string {
	return ClientTypeStrings[t]
}

func ClientTypeFromString(s string) (ClientType, error) {
	for i, v := range ClientTypeStrings {
		if v == s {
			return ClientType(i), nil
		}
	}
	return -1, fmt.Errorf("unknown ClientType(%v)", s)
}

func (t ClientType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

func (t *ClientType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	} else if clientType, err := ClientTypeFromString(s); err != nil {
		return err
	} else {
		*t = clientType
		return nil
	}
}

//Client is websocker client
type Client struct {
	registerHub                 *RegisterHub
	broadcastHub                *BroadcastHub
	conn                        *websocket.Conn
	send                        chan []byte
	ID                          string
	Type                        ClientType
	ConnectionId                string
	Name                        string
	Surname                     string
	AvatarSrc                   string
	AccessToken                 string
	LastPositionX               float64
	LastPositionY               float64
	PrevPositionX               float64
	PrevPositionY               float64
	isInSpeakerLocation         bool
	isInQuietLocation           bool
	LastRules                   []string
	LastRadarRules              []string
	LastRadarVolume             []radarVolumeStruct
	radar                       chan RadarRequest
	Version                     int
	Expired                     bool
	Inited                      bool
	Jitsi                       bool
	Video                       bool
	Audio                       bool
	PhoneCall                   bool
	isAdmin                     bool
	isOwner                     bool
	isSpecialGuest              bool
	Mode                        string
	PopupTime                   int64
	InvitedToStage              bool
	NormalClosure               bool
	Restoring                   bool
	Badges                      []string
	HandUpNotificationTime      time.Time
	CallToStageNotificationTime sync.Map
}

type radarVolumeStruct struct {
	Volume json.Number `json:"volume"`
	ID     string      `json:"id"`
}

//RadarRequest request radar send
type RadarRequest struct {
	Participants []string
	RadarVolume  []radarVolumeStruct
	Delay        int
	IsSubscriber bool
	ScreenVolume float64
}

//HubMessage is message with sender Client
type HubMessage struct {
	Client  *Client
	Message []byte
}

//ErrorResponse used for returning error information to client
type ErrorResponse struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

//ClientRequest typed json from client
type ClientRequest struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

//ClientResponse typed json to client
type ClientResponse struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

//ClientResponseConnectionState typed json to client
type ClientResponseConnectionState struct {
	ID    string           `json:"id"`
	State string           `json:"state"`
	Mode  string           `json:"mode"`
	User  ShortParticipant `json:"user"`
}

//ClientResponseState typed json to client
type ClientResponseState struct {
	Room         map[string]Participant      `json:"room"`
	Popup        map[string]ShortParticipant `json:"popup"`
	Current      Participant                 `json:"current"`
	Admins       []string                    `json:"admins"`
	Hands        []string                    `json:"hands"`
	OwnerID      string                      `json:"owner"`
	OnlineAdmins []string                    `json:"onlineAdmins"`
}

//ClientResponseState5 typed json to client
type ClientResponseState5 struct {
	Room                   map[string]Participant `json:"room"`
	Popup                  []ShortParticipant     `json:"popup"`
	ListenersCount         int                    `json:"listenersCount"`
	Current                Participant            `json:"current"`
	Admins                 []string               `json:"admins"`
	Hands                  []string               `json:"hands"`
	HandsAllowed           bool                   `json:"handsAllowed"`
	OwnerID                string                 `json:"owner"`
	OnlineAdmins           []string               `json:"onlineAdmins"`
	AbsoluteSpeakerPresent bool                   `json:"absoluteSpeakerPresent"`
}

//BroadcastRequest typed json from client
type BroadcastRequest struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

//ClientBroadcastResponse typed json to client
type ClientBroadcastResponse struct {
	Type    string          `json:"type"`
	From    string          `json:"from"`
	Payload json.RawMessage `json:"payload"`
}

//ClientRequestRegister with access token
type ClientRequestRegister struct {
	SID         string `json:"sid"`
	AccessToken string `json:"accessToken"`
	Version     int    `json:"version"`
	Password    string `json:"password"`
}

//ClientRequestParticipant with id
type ClientRequestParticipant struct {
	ID string `json:"id"`
}

//ClientRequestAudioLevel with coords
type ClientRequestAudioLevel struct {
	Value json.Number `json:"value"`
}

type ClientRequestBecomeAbsoluteSpeaker struct {
	State bool `json:"state"`
}

//ClientRequestUserState with jitsi
type ClientRequestUserState struct {
	Jitsi     bool `json:"jitsi"`
	Video     bool `json:"video"`
	Audio     bool `json:"audio"`
	PhoneCall bool `json:"phoneCall"`
}

//ClientRequestStage with id of user
type ClientRequestStage struct {
	ID string `json:"id"`
}

type ClientRequestDeclineStage struct {
	InviterID string `json:"inviterId"`
}

//ClientRequestMute with user
type ClientRequestMute struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

//ClientResponseMute with user
type ClientResponseMute struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	FromName    string `json:"fromName"`
	FromSurname string `json:"fromSurname"`
}

//ClientRequestHand with id of user
type ClientRequestHand struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type ClientRequestSetHandsAllowed struct {
	Value bool `json:"value"`
}

//ServerHandNotify with id of user
type ServerHandNotify struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	FromId      string `json:"fromId"`
	FromName    string `json:"fromName"`
	FromSurname string `json:"fromSurname"`
}

//ClientRequestPath with coords
type ClientRequestPath struct {
	CurrentX json.Number `json:"currentX"`
	CurrentY json.Number `json:"currentY"`
	NextX    json.Number `json:"nextX"`
	NextY    json.Number `json:"nextY"`
	Type     string      `json:"type"`
}

//ClientResponsePath with sender id
type ClientResponsePath struct {
	ID     string  `json:"id"`
	Points []Point `json:"points"`
}

//Point coords
type Point struct {
	X        json.Number `json:"x"`
	Y        json.Number `json:"y"`
	Duration json.Number `json:"duration"`
	Index    int         `json:"index"`
}

//ClientRequestSubscriptions with user ids
type ClientRequestSubscriptions struct {
	Participants []string
	Distances    map[string]float64
}

//ClientResponseRegister with sender id
type ClientResponseRegister struct {
	ID   string     `json:"id"`
	Type ClientType `json:"type"`
}

//ClientResponseStatus with status
type ClientResponseStatus struct {
	Status bool `json:"status"`
}

//ServerRequestRadar waitg participants
type ServerRequestRadar struct {
	Participants []string `json:"participants"`
	IsSubscriber bool     `json:"isSubscriber"`
}

//ServerRequestRadarVolume waitg participants
type ServerRequestRadarVolume struct {
	Participants []string            `json:"participants"`
	IsSubscriber bool                `json:"isSubscriber"`
	RadarVolume  []radarVolumeStruct `json:"radarVolume"`
	ScreenVolume json.Number         `json:"screenVolume"`
}

//APICurrentResponse api response
type APICurrentResponse struct {
	Response CurrentResponse `json:"response"`
}

//CurrentResponse api response current
type CurrentResponse struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Surname   string   `json:"surname"`
	AvatarSrc string   `json:"avatarSrc"`
	Badges    []string `json:"badges"`
}

//APIRoomResponse api response
type APIRoomResponse struct {
	Response RoomResponse `json:"response"`
}

//RoomResponse api response room
type RoomResponse struct {
	ID            int        `json:"id"`
	Config        RoomConfig `json:"config"`
	AdminsIDs     []int      `json:"adminsIds"`
	SpeakerIds    []int      `json:"speakerIds"`
	SpecialGuests []int      `json:"specialGuests"`
	OwnerID       int        `json:"ownerId"`
}

//RoomConfig api response room config
type RoomConfig struct {
	PublisherRadarSize json.Number                     `json:"publisherRadarSize"`
	VideoBubbleSize    json.Number                     `json:"videoBubbleSize"`
	BackgroundRoom     BackgroundRoomConfig            `json:"backgroundRoom"`
	Objects            map[string]RoomConfigObject     `json:"objects"`
	ObjectsData        map[string]RoomConfigObjectData `json:"objectsData"`
	WithSpeakers       bool                            `json:"withSpeakers"`
}

//BackgroundRoomConfig api response room config background
type BackgroundRoomConfig struct {
	Width  json.Number `json:"width"`
	Height json.Number `json:"height"`
}

//RoomConfigObject api response room config object
type RoomConfigObject struct {
	X      json.Number `json:"x"`
	Y      json.Number `json:"y"`
	Type   string      `json:"type"`
	Width  json.Number `json:"width"`
	Height json.Number `json:"height"`
}

type RoomObject interface {
	isInside(x, y float64) bool
}

//CircleRoomObject
type CircleRoomObject struct {
	X      float64
	Y      float64
	Radius float64
}

func (c CircleRoomObject) isInside(x, y float64) bool {
	sdist := GetDistance(c.X, c.Y, x, y)
	return sdist <= c.Radius
}

//RectangleRoomObject
type RectangleRoomObject struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

func (r RectangleRoomObject) isInside(x, y float64) bool {
	return x >= r.X && x <= r.X+r.Width &&
		y >= r.Y && y <= r.Y+r.Height
}

type RoomLocation []RoomObject

func (loc RoomLocation) isInside(x, y float64) bool {
	if loc == nil {
		return false
	}
	for _, o := range loc {
		if o.isInside(x, y) {
			return true
		}
	}
	return false
}

//RoomConfigObjectData api response room config object
type RoomConfigObjectData struct {
	Radius json.Number `json:"radius"`
	Length json.Number `json:"length"`
}

//Participant data
type Participant struct {
	Name              string      `json:"name"`
	Surname           string      `json:"surname"`
	Avatar            string      `json:"avatar"`
	X                 json.Number `json:"x"`
	Y                 json.Number `json:"y"`
	IsSpeaker         bool        `json:"isSpeaker"`
	AudioLevel        int64       `json:"audioLevel"`
	Expired           bool        `json:"expired"`
	InRadar           bool        `json:"inRadar"`
	Volume            json.Number `json:"volume"`
	Jitsi             bool        `json:"jitsi"`
	Video             bool        `json:"video"`
	Audio             bool        `json:"audio"`
	PhoneCall         bool        `json:"phoneCall"`
	Mode              string      `json:"mode"`
	ID                string      `json:"id"`
	IsAdmin           bool        `json:"isAdmin"`
	IsOwner           bool        `json:"isOwner"`
	IsSpecialGuest    bool        `json:"isSpecialGuest"`
	IsAbsoluteSpeaker bool        `json:"isAbsoluteSpeaker"`
	Badges            []string    `json:"badges"`
}

//ShortParticipant data
type ShortParticipant struct {
	Name           string   `json:"name"`
	Surname        string   `json:"surname"`
	Avatar         string   `json:"avatar"`
	ID             string   `json:"id"`
	IsAdmin        bool     `json:"isAdmin"`
	IsOwner        bool     `json:"isOwner"`
	IsSpecialGuest bool     `json:"isSpecialGuest"`
	Badges         []string `json:"badges"`
}

//RegisterHub for clients
type RegisterHub struct {
	incoming         chan HubMessage
	hubs             sync.Map
	newRelic         *newrelicV3.Application
	unregisterHub    chan string
	unregisterClient chan *Client
	registerClient   chan *Client
	clients          sync.Map
	events           chan Event
}

//BroadcastHub for clients
type BroadcastHub struct {
	clients            sync.Map
	incoming           chan HubMessage
	register           chan *Client
	unregister         chan *Client
	sid                string
	positions          chan *Client
	stateRequests      chan bool
	publisherRadarSize float64
	newRelic           *newrelicV3.Application
	registerHub        *RegisterHub
	speakerLocation    RoomLocation
	quietLocation      RoomLocation
	screenLocationX    float64
	screenLocationY    float64
	nowSpeaking        sync.Map
	bubleRadius        float64
	quit               chan bool
	width              float64
	height             float64
	mainSpawnX1        float64
	mainSpawnX2        float64
	mainSpawnY1        float64
	mainSpawnY2        float64
	staticObjects      []CircleRoomObject
	withSpeakers       bool
	AdminsIDs          sync.Map
	SpeakerIDs         sync.Map
	Hands              sync.Map
	handsAllowed       bool
	handsAllowedMu     sync.Mutex
	BanIDs             sync.Map
	EmojiStat          sync.Map
	OwnerID            string
	SpecialGuestIDs    set.StringSet
	allSpeakers        bool
	AbsoluteSpeakerID  string
	AbsoluteSpeakerMu  sync.Mutex
}

type nowSpeakingStruct struct {
	Volume    int64
	TimeStamp time.Time
}

//APIClient is http client for calling external services
type APIClient struct {
	newRelic *newrelicV3.Application
}

//SubscriberSorting for sorting
type SubscriberSorting struct {
	ID       string
	Priority int
	Dist     float64
}

type Event interface {
	name() string
}

type StateEvent struct {
	Sid      string   `json:"sid"`
	Speakers []string `json:"speakers"`
}

func (e *StateEvent) name() string {
	return "state"
}

type NewBroadcastHubEvent struct {
	Sid string `json:"sid"`
}

func (e *NewBroadcastHubEvent) name() string {
	return "new-broadcast-hub"
}
