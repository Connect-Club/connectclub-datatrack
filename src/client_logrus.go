package main

import (
	"context"
	"github.com/sirupsen/logrus"
)

type ContextClientHook struct {}

func (h *ContextClientHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *ContextClientHook) Fire(e *logrus.Entry) error {
	if e.Context != nil {
		if client := e.Context.Value("client"); client != nil {
			if clientCasted, ok := client.(*Client); ok {
				e.Data["clientId"] = clientCasted.ID
				e.Data["clientConnectionId"] = clientCasted.ConnectionId
			}
		}
	}
	return nil
}

func setClient(ctx context.Context, client *Client) context.Context {
	if ctx.Value("client") != client {
		ctx = context.WithValue(ctx, "client", client)
	}
	return ctx
}