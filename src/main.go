package main

import (
	"cloud.google.com/go/pubsub"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/kelseyhightower/envconfig"
	"github.com/natefinch/lumberjack"
	_ "github.com/newrelic/go-agent"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"io"
	"math"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// 1/4872*1000 (1 second for moving one screen height iphone x)
var bubleSpeed float64 = 0.20525451

var config Config
var subscriptionsGauge prometheus.Gauge
var subscriptionsCounter prometheus.Counter
var usersGauge prometheus.Gauge
var roomsGauge prometheus.Gauge
var serviceAccessToken string

func serveHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "home.html")
}

// serveWs handles websocket requests from the peer.
func serveWs(ctx context.Context, registerHub *RegisterHub, w http.ResponseWriter, r *http.Request) {
	log := logrus.WithContext(ctx)

	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Error("cannot upgrade the HTTP server connection to the WebSocket protocol")
		return
	}
	client := &Client{
		registerHub:     registerHub,
		broadcastHub:    nil,
		conn:            conn,
		send:            make(chan []byte, 256),
		ID:              "0",
		Type:            ClientTypeNil,
		ConnectionId:    uuid.New().String(),
		Name:            "",
		Surname:         "",
		AvatarSrc:       "",
		AccessToken:     "",
		LastPositionX:   0.0,
		LastPositionY:   0.0,
		PrevPositionX:   0.0,
		PrevPositionY:   0.0,
		LastRules:       []string{"0"},
		LastRadarRules:  []string{},
		LastRadarVolume: []radarVolumeStruct{},
		radar:           make(chan RadarRequest, 256),
		Version:         1,
		Expired:         false,
		Inited:          false,
		Jitsi:           false,
		Video:           true,
		Audio:           true,
		PhoneCall:       false,
		isAdmin:         false,
		isOwner:         false,
		isSpecialGuest:  false,
		Mode:            "room",
		PopupTime:       time.Now().UnixNano(),
		InvitedToStage:  false,
		NormalClosure:   false,
		Restoring:       false,
		Badges:          []string{},
	}
	registerHub.registerClient <- client
	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump(ctx)
	go client.readPump(ctx)
	go client.radarSender(ctx)
}

func main() {
	lumberjackLogger := &lumberjack.Logger{
		Filename:   filepath.Join(".", "logs", "json.log"),
		MaxBackups: 10,
	}
	logrus.SetOutput(io.MultiWriter(
		os.Stdout,
		lumberjackLogger,
	))
	logrus.AddHook(&ContextFieldsHook{})
	logrus.AddHook(&ContextClientHook{})
	logrus.AddHook(&ContextBroadcastHubHook{})
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&logrus.JSONFormatter{
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime: "@timestamp",
			logrus.FieldKeyMsg:  "message",
		},
	})

	if value, ok := os.LookupEnv("SERVICE_ACCESS_TOKEN"); ok {
		serviceAccessToken = value
	} else {
		logrus.Warn("env variable SERVICE_ACCESS_TOKEN is not set")
		serviceAccessToken = "service-access-token"
	}

	ctx := addLogrusField(context.Background(), "appId", uuid.New().String())
	log := logrus.WithContext(ctx)

	go func() {
		pprofRouter := mux.NewRouter()
		pprofRouter.PathPrefix("/debug/").Handler(http.DefaultServeMux)

		err := http.ListenAndServe(":8082", pprofRouter)
		if err != nil {
			log.WithError(err).Fatal("pprof")
		}
	}()

	regProm()
	log.Info("app start")
	rand.Seed(time.Now().UnixNano())

	confErr := envconfig.Process("datatrack", &config)
	if confErr != nil {
		log.WithError(confErr).Fatal("main")
	}

	registerHub := newRegisterHub()

	if os.Getenv("DISABLE_NEWRELIC") != "true" {
		app, nrErr := newrelic.NewApplication(
			newrelic.ConfigAppName(config.NewRelicName),
			newrelic.ConfigLicense(config.NewRelicLicense),
			newrelic.ConfigDistributedTracerEnabled(true),
		)

		if nrErr != nil {
			log.WithError(nrErr).Fatal("main")
		}
		registerHub.newRelic = app
	}

	go registerHub.runRegisterHub(ctx)

	router := mux.NewRouter()

	router.HandleFunc(newrelic.WrapHandleFunc(registerHub.newRelic, "/", serveHome))
	router.HandleFunc(newrelic.WrapHandleFunc(registerHub.newRelic, "/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(ctx, registerHub, w, r)
	}))
	go starProm(ctx)
	go startAPI(ctx, registerHub, registerHub.newRelic)
	go startPubSub(ctx, registerHub)

	err := http.ListenAndServe(":8080", router)
	if err != nil {
		log.WithError(err).Fatal("main")
	}

}

func regProm() {
	subscriptionsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "connect",
		Subsystem: "datatrack",
		Name:      "subscriptionsGauge",
		Help:      "Number of change subscriptions requests.",
	})

	subscriptionsCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "connect",
		Subsystem: "datatrack",
		Name:      "subscriptionsTotal",
		Help:      "Number of change subscriptions requests.",
	})

	usersGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "connect",
		Subsystem: "datatrack",
		Name:      "usersGauge",
		Help:      "Number of online users.",
	})

	roomsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "connect",
		Subsystem: "datatrack",
		Name:      "roomsGauge",
		Help:      "Number of online rooms.",
	})
	prometheus.MustRegister(usersGauge, roomsGauge, subscriptionsCounter, subscriptionsGauge)
}

func starProm(ctx context.Context) {
	log := logrus.WithContext(ctx)

	err := http.ListenAndServe(":8081", promhttp.Handler())
	if err != nil {
		log.WithError(err).Fatal("starProm")
	}
}

func startAPI(ctx context.Context, registerHub *RegisterHub, app *newrelic.Application) {
	log := logrus.WithContext(ctx)

	router := mux.NewRouter()

	router.HandleFunc(newrelic.WrapHandleFunc(app, "/info", func(w http.ResponseWriter, r *http.Request) {
		infoAPI(ctx, registerHub, w, r)
	}))

	router.HandleFunc(newrelic.WrapHandleFunc(app, "/ban", func(w http.ResponseWriter, r *http.Request) {
		banAPI(ctx, registerHub, w, r)
	}))

	router.HandleFunc(newrelic.WrapHandleFunc(app, "/info2", func(w http.ResponseWriter, r *http.Request) {
		info2API(ctx, registerHub, w, r)
	}))

	err := http.ListenAndServe(":8083", router)
	if err != nil {
		log.WithError(err).Fatal("startApi")
	}
}

func startPubSub(ctx context.Context, registerHub *RegisterHub) {
	log := logrus.WithContext(ctx)

	hostname := os.Getenv("INGRESS_HOST")
	if hostname == "" {
		log.Fatal("seems like INGRESS_HOST env var is not set")
	} else {
		log.Infof("INGRESS_HOST=%v", hostname)
	}

	projectId := os.Getenv("GCLOUD_PROJECT_ID")
	if projectId == "" {
		log.Fatal("seems like GCLOUD_PROJECT_ID env var is not set")
	} else {
		log.Infof("GCLOUD_PROJECT_ID=%v", projectId)
	}
	client, err := pubsub.NewClient(ctx, projectId)
	if err != nil {
		log.WithError(err).Fatal("cannot create pubsub client")
	}
	defer client.Close()
	topic := client.Topic("datatrack")

	for event := range registerHub.events {
		data, err := json.Marshal(event)
		if err != nil {
			log.WithError(err).Error("cannot marshal event")
			continue
		}
		result := topic.Publish(ctx, &pubsub.Message{
			Attributes: map[string]string{"eventName": event.name(), "hostname": hostname},
			Data:       data,
		})
		if _, err := result.Get(ctx); err != nil {
			log.WithError(err).Error("cannot publish event")
		}
	}
}

func infoAPI(ctx context.Context, registerHub *RegisterHub, w http.ResponseWriter, r *http.Request) {
	log := logrus.WithContext(ctx)

	jsonReq := struct {
		Names []string `json:"names"`
	}{}

	err := json.NewDecoder(r.Body).Decode(&jsonReq)
	if err != nil {
		log.WithError(err).Warn("cannot decode request")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jsonResp, err := registerHub.getInfo(jsonReq.Names)
	if err != nil {
		log.WithError(err).Warn("getInfo")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResp)
}

func info2API(ctx context.Context, registerHub *RegisterHub, w http.ResponseWriter, r *http.Request) {
	log := logrus.WithContext(ctx)

	jsonResp, err := registerHub.getInfo2()
	if err != nil {
		log.WithError(err).Warn("getInfo2")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResp)
}

func banAPI(ctx context.Context, registerHub *RegisterHub, w http.ResponseWriter, r *http.Request) {
	log := logrus.WithContext(ctx)

	d := json.NewDecoder(r.Body)

	jsonReq := struct {
		RoomSid string `json:"roomSid"`
		UserID  string `json:"userId"`
	}{}

	err := d.Decode(&jsonReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Infof("try: roomSid=%v, userId=%v", jsonReq.RoomSid, jsonReq.UserID)
	registerHub.banUser(ctx, jsonReq.RoomSid, jsonReq.UserID)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("{}"))
}

//EqualRules array
func EqualRules(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

//GetDistance between points
func GetDistance(x1, y1, x2, y2 float64) float64 {
	dx := x2 - x1
	dy := y2 - y1
	d := math.Sqrt(dx*dx + dy*dy)
	return math.Max(d, 0.1)
}

//Float64ToNumber get json number string
func Float64ToNumber(x float64) json.Number {
	return json.Number(strconv.FormatFloat(x, 'f', -1, 64))
}

//RandFloats get random floats
func RandFloats(min, max float64, n int) []float64 {
	res := make([]float64, n)
	for i := range res {
		res[i] = min + rand.Float64()*(max-min)
	}
	return res
}

//RandInts get random floats
func RandInts(min, max float64, n int) []int {
	res := make([]int, n)
	for i := range res {
		r := min + rand.Float64()*(max-min)
		res[i] = int(r)
	}
	return res
}

func removeDuplicateStringValues(stringSlice []string) []string {
	keys := make(map[string]bool)
	list := []string{}

	for _, entry := range stringSlice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func removeValueFromArray(stringSlice []string, toRemove string) []string {
	list := []string{}

	for _, entry := range stringSlice {
		if entry != toRemove {
			list = append(list, entry)
		}
	}
	return list
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
