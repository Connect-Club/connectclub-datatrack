package main

import (
	"context"
	"github.com/sirupsen/logrus"
)

type ContextBroadcastHubHook struct{}

func (h *ContextBroadcastHubHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *ContextBroadcastHubHook) Fire(e *logrus.Entry) error {
	if e.Context != nil {
		if client := e.Context.Value("broadcastHub"); client != nil {
			if clientCasted, ok := client.(*BroadcastHub); ok {
				e.Data["sid"] = clientCasted.sid
			}
		}
	}
	return nil
}

func setBroadcastHub(ctx context.Context, h *BroadcastHub) context.Context {
	if ctx.Value("broadcastHub") != h {
		ctx = context.WithValue(ctx, "broadcastHub", h)
	}
	return ctx
}