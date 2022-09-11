package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	newrelicV3 "github.com/newrelic/go-agent/v3/newrelic"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"time"
)

func (api *APIClient) doRequest(req *http.Request, duration time.Duration, name string) (*http.Response, error) {
	txn := api.newRelic.StartTransaction(name)

	client := &http.Client{Timeout: duration * time.Second}
	client.Transport = newrelicV3.NewRoundTripper(client.Transport)

	res, err := client.Do(req)

	if nil != err {
		txn.NoticeError(err)
	}
	txn.End()

	return res, err
}

func (api *APIClient) newRequest(method string, url string, body io.Reader) (*http.Request, error) {
	req, reqErr := http.NewRequest(method, url, body)

	if reqErr != nil {
		return nil, reqErr
	}

	return req, nil
}

func (api *APIClient) sendEvent(ctx context.Context, sid string, event string, clientID string) {
	log := logrus.WithContext(ctx)

	reqBody, err := json.Marshal(map[string]string{
		"event":  event,
		"userId": clientID,
	})

	req, err := api.newRequest("POST", config.APIURL+"/api/v1/event/"+sid+"/statistic", bytes.NewBuffer(reqBody))
	if err != nil {
		log.WithError(err).Error("newRequest")
	}

	_, err = api.doRequest(req, 1, "sendEvent")
	if err != nil {
		log.WithError(err).Error("doRequest")
	}
}

func (api *APIClient) getUserID(ctx context.Context, accessToken string) (APICurrentResponse, error) {
	log := logrus.WithContext(ctx)

	var apiCurrentResponse APICurrentResponse

	getURL := config.APIURL + "/api/v1/account/current"
	var bearer = "Bearer " + accessToken
	req, reqErr := api.newRequest("GET", getURL, nil)
	if reqErr != nil {
		log.WithError(reqErr).Error("newRequest")
		return apiCurrentResponse, errors.New("getUserID reqErr")
	}
	req.Header.Add("Authorization", bearer)
	resp, httpErr := api.doRequest(req, 5, "getUserID")
	if httpErr != nil {
		log.WithError(httpErr).Error("doRequest")
		return apiCurrentResponse, errors.New("getUserID httpErr")
	}

	statusOK := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !statusOK {
		log.Errorf("not 2xx code, response=%v", resp)
		return apiCurrentResponse, errors.New("getUserID httpErr")
	}

	body, _ := ioutil.ReadAll(resp.Body)
	jsonErr := json.Unmarshal(body, &apiCurrentResponse)
	if jsonErr != nil {
		log.WithError(jsonErr).Warn("cannot unmarshal APICurrentResponse")
		return apiCurrentResponse, errors.New("getUserID jsonErr")
	}
	return apiCurrentResponse, nil
}

func (api *APIClient) getRoomByName(ctx context.Context, accessToken string, name string, password string) (APIRoomResponse, error) {
	log := logrus.WithContext(ctx)

	var apiRoomResponse APIRoomResponse

	getURL := config.APIURL + "/api/v1/video-room/" + name + "?password=" + password
	var bearer = "Bearer " + accessToken
	req, reqErr := api.newRequest("GET", getURL, nil)
	if reqErr != nil {
		log.WithError(reqErr).Error("newRequest")
		return apiRoomResponse, errors.New("getRoomByName reqErr")
	}
	req.Header.Add("Authorization", bearer)
	resp, httpErr := api.doRequest(req, 5, "getRoomByName")
	if httpErr != nil {
		log.WithError(httpErr).Error("doRequest")
		return apiRoomResponse, errors.New("getRoomByName httpErr")
	}
	body, _ := ioutil.ReadAll(resp.Body)
	jsonErr := json.Unmarshal(body, &apiRoomResponse)
	if jsonErr != nil {
		log.WithError(jsonErr).Error("cannot unmarshal APIRoomResponse")
		return apiRoomResponse, errors.New("getRoomByName jsonErr")
	}

	return apiRoomResponse, nil
}

func (api *APIClient) promoteUser(ctx context.Context, roomID string, userID string) {
	log := logrus.WithContext(ctx)

	postURL := config.APIURL + "/api/v1/event/" + roomID + "/" + userID + "/promote"

	req, reqErr := api.newRequest("POST", postURL, bytes.NewBuffer([]byte("")))

	if reqErr != nil {
		log.WithError(reqErr).Error("newRequest")
	}

	req.Header.Set("Content-Type", "application/json")

	_, httpErr := api.doRequest(req, 5, "promoteUser")

	if httpErr != nil {
		log.WithError(httpErr).Error("doRequest")
	}
}

func (api *APIClient) demoteUser(ctx context.Context, roomID string, userID string) {
	log := logrus.WithContext(ctx)

	postURL := config.APIURL + "/api/v1/event/" + roomID + "/" + userID + "/demote"

	req, reqErr := api.newRequest("POST", postURL, bytes.NewBuffer([]byte("")))

	if reqErr != nil {
		log.WithError(reqErr).Error("newRequest")
	}

	req.Header.Set("Content-Type", "application/json")

	_, httpErr := api.doRequest(req, 5, "demoteUser")

	if httpErr != nil {
		log.WithError(httpErr).Error("doRequest")
	}
}
