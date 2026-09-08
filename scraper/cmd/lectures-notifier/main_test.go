package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name       string
		attempt    int
		statusCode int
		want       time.Duration
	}{
		{
			name:       "standard network error attempt 1",
			attempt:    1,
			statusCode: 0,
			want:       1 * time.Second,
		},
		{
			name:       "standard network error attempt 2",
			attempt:    2,
			statusCode: 0,
			want:       2 * time.Second,
		},
		{
			name:       "standard network error attempt 3",
			attempt:    3,
			statusCode: 0,
			want:       4 * time.Second,
		},
		{
			name:       "rate limited 429 attempt 1",
			attempt:    1,
			statusCode: http.StatusTooManyRequests,
			want:       1 * time.Second,
		},
		{
			name:       "502 bad gateway attempt 1",
			attempt:    1,
			statusCode: http.StatusBadGateway,
			want:       10 * time.Second,
		},
		{
			name:       "502 bad gateway attempt 2",
			attempt:    2,
			statusCode: http.StatusBadGateway,
			want:       20 * time.Second,
		},
		{
			name:       "502 bad gateway attempt 3",
			attempt:    3,
			statusCode: http.StatusBadGateway,
			want:       40 * time.Second,
		},
		{
			name:       "503 service unavailable attempt 1",
			attempt:    1,
			statusCode: http.StatusServiceUnavailable,
			want:       10 * time.Second,
		},
		{
			name:       "500 internal server error attempt 1",
			attempt:    1,
			statusCode: http.StatusInternalServerError,
			want:       10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateBackoff(tt.attempt, tt.statusCode)
			if got != tt.want {
				t.Errorf("calculateBackoff(%d, %d) = %v; want %v", tt.attempt, tt.statusCode, got, tt.want)
			}
		})
	}
}

func TestIsUnrecoverable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "502 error",
			err:  errors.New("eventbrite status 502: Bad Gateway"),
			want: false,
		},
		{
			name: "503 error",
			err:  errors.New("eventbrite status 503: Service Unavailable"),
			want: false,
		},
		{
			name: "500 error",
			err:  errors.New("eventbrite status 500: Internal Server Error"),
			want: false,
		},
		{
			name: "network timeout",
			err:  errors.New("dial tcp 10.0.0.1:443: i/o timeout"),
			want: false,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "401 unauthorized in message",
			err:  errors.New("eventbrite status 401: Unauthorized"),
			want: true,
		},
		{
			name: "403 forbidden in message",
			err:  errors.New("eventbrite status 403: Forbidden"),
			want: true,
		},
		{
			name: "invalid credentials in message",
			err:  errors.New("invalid credentials provided"),
			want: true,
		},
		{
			name: "explicitly marked unrecoverable wrapped",
			err:  fmt.Errorf("fetching failed: %w", markUnrecoverable(errors.New("permanent error"))),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUnrecoverable(tt.err)
			if got != tt.want {
				t.Errorf("isUnrecoverable(%v) = %v; want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestDetermineHealthcheckSuffix(t *testing.T) {
	tests := []struct {
		name           string
		runErr         error
		panicVal       interface{}
		wantSuffix     string
		wantShouldPing bool
	}{
		{
			name:           "success run",
			runErr:         nil,
			panicVal:       nil,
			wantSuffix:     "",
			wantShouldPing: true,
		},
		{
			name:           "transient 502 error skips ping",
			runErr:         errors.New("eventbrite status 502: Bad Gateway"),
			panicVal:       nil,
			wantSuffix:     "",
			wantShouldPing: false,
		},
		{
			name:           "transient network timeout skips ping",
			runErr:         errors.New("dial tcp: i/o timeout"),
			panicVal:       nil,
			wantSuffix:     "",
			wantShouldPing: false,
		},
		{
			name:           "transient context deadline exceeded skips ping",
			runErr:         context.DeadlineExceeded,
			panicVal:       nil,
			wantSuffix:     "",
			wantShouldPing: false,
		},
		{
			name:           "unrecoverable 401 error pings fail",
			runErr:         errors.New("eventbrite status 401: Unauthorized"),
			panicVal:       nil,
			wantSuffix:     "fail",
			wantShouldPing: true,
		},
		{
			name:           "panic without runErr pings fail",
			runErr:         nil,
			panicVal:       "nil pointer dereference",
			wantSuffix:     "fail",
			wantShouldPing: true,
		},
		{
			name:           "panic with runErr pings fail",
			runErr:         errors.New("panic: nil pointer dereference"),
			panicVal:       "nil pointer dereference",
			wantSuffix:     "fail",
			wantShouldPing: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSuffix, gotShouldPing := determineHealthcheckSuffix(tt.runErr, tt.panicVal)
			if gotSuffix != tt.wantSuffix || gotShouldPing != tt.wantShouldPing {
				t.Errorf("determineHealthcheckSuffix(%v, %v) = (%q, %v); want (%q, %v)",
					tt.runErr, tt.panicVal, gotSuffix, gotShouldPing, tt.wantSuffix, tt.wantShouldPing)
			}
		})
	}
}
