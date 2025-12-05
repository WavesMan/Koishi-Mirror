package logging

import (
	"context"
	"encoding/json"
	"fmt"
	pgstore "npm-mirror/internal/storage/postgres"
	"strings"
	"time"
)

type Logger struct {
	level int
	store *pgstore.Store
}

func New(level string, store *pgstore.Store) *Logger {
	l := strings.ToLower(level)
	n := 2
	switch l {
	case "trace":
		n = 0
	case "debug":
		n = 1
	case "info":
		n = 2
	case "warn":
		n = 3
	case "error":
		n = 4
	}
	return &Logger{level: n, store: store}
}

func (l *Logger) enabled(n int) bool { return n >= l.level }

func (l *Logger) log(n int, level, category, msg string, fields map[string]interface{}) {
	if !l.enabled(n) {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	var kv string
	if len(fields) > 0 {
		b, _ := json.Marshal(fields)
		kv = string(b)
	}
	if kv != "" {
		fmt.Printf("[%s] %s %s %s %s\n", ts, strings.ToUpper(level), category, msg, kv)
	} else {
		fmt.Printf("[%s] %s %s %s\n", ts, strings.ToUpper(level), category, msg)
	}
	if l.store != nil {
		_ = l.store.InsertLog(context.Background(), level, category, msg, fields)
	}
}

func (l *Logger) Trace(
	category,
	msg string,
	fields map[string]interface{}) {
	l.log(
		0,
		"trace", category, msg, fields,
	)
}
func (l *Logger) Debug(category, msg string, fields map[string]interface{}) {
	l.log(
		1,
		"debug", category, msg, fields,
	)
}
func (l *Logger) Info(category, msg string, fields map[string]interface{}) {
	l.log(
		2,
		"info", category, msg, fields,
	)
}
func (l *Logger) Warn(category, msg string, fields map[string]interface{}) {
	l.log(
		3,
		"warn", category, msg, fields,
	)
}
func (l *Logger) Error(category, msg string, fields map[string]interface{}) {
	l.log(
		4,
		"error", category, msg, fields,
	)
}
