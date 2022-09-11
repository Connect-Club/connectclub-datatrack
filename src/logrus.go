package main

import (
	"context"
	"github.com/sirupsen/logrus"
)

type ContextFieldsHook struct {}

func (h *ContextFieldsHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *ContextFieldsHook) Fire(e *logrus.Entry) error {
	if e.Context != nil {
		if logrusFields := e.Context.Value("logrusFields"); logrusFields != nil {
			if logrusFieldsCasted, ok := logrusFields.(logrus.Fields); ok {
				for k, v := range logrusFieldsCasted {
					e.Data[k] = v
				}
			}
		}
	}
	return nil
}



func addLogrusContextFields(ctx context.Context, fields logrus.Fields) context.Context {
	logrusFields := ctx.Value("logrusFields")
	if logrusFields != nil {
		logrusFieldsCasted, ok := logrusFields.(logrus.Fields)
		if !ok {
			panic("logrusFields wrong type")
		}
		for k, v := range logrusFieldsCasted {
			if _, ok := fields[k]; !ok {
				fields[k] = v
			}
		}
	}
	return context.WithValue(ctx, "logrusFields", fields)
}

func addLogrusField(ctx context.Context, key string, value interface{}) context.Context {
	return addLogrusContextFields(ctx, logrus.Fields{key: value})
}
