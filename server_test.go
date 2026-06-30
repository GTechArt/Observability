package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/buffer"
)

func Test_resquestLogger(t *testing.T) {
	logBuffer := &buffer.Buffer{}

	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Time(slog.TimeKey, time.Date(2023, 10, 1, 12, 34, 57, 0, time.UTC))
			}
			return a
		},
	}))

	requestLoggerMiddleware := requestLogger(logger)
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	loggedHandler := requestLoggerMiddleware(dummyHandler)

	req := httptest.NewRequest("GET", "http://lin.ko/api/stats", nil)
	rr := httptest.NewRecorder()
	loggedHandler.ServeHTTP(rr, req)

	log := logBuffer.String()
	checks := []string{
		`time=2023-10-01T12:34:57.000Z`,
		`method=GET`,
		`path=/api/stats`,
		`client_ip=192.0.2.x`,
		`request_body_bytes=0`,
		`response_status=200`,
		`response_body_bytes=0`,
		`request_id=""`,
	}
	for _, s := range checks {
		if !strings.Contains(log, s) {
			t.Errorf("Log missing %q in:\n%v", s, log)
		}
	}
	if rr.Code != http.StatusOK {
		t.Errorf("Response code is: %v, expected : %v", rr.Code, http.StatusOK)
	}
}
