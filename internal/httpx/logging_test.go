package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func captureRequestLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var output bytes.Buffer

	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })

	return &output
}

func requestLog(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()

	var entry map[string]any

	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("invalid log entry: %v; %s", err, output)
	}

	return entry
}

func TestRequestLogsCaptureMetadataWithoutRequestContents(t *testing.T) {
	output := captureRequestLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, "OK")
	}))
	r := httptest.NewRequest("POST", "/requests?api_key=query-secret", strings.NewReader(`{"account_number":"0123454821","prompt":"private-prompt"}`))
	r.Header.Set("Authorization", "Bearer header-secret")
	r.Header.Set("Idempotency-Key", "private-operation-key")
	h.ServeHTTP(httptest.NewRecorder(), r)

	entry := requestLog(t, output)

	if entry["msg"] != "HTTP request" || entry["method"] != "POST" || entry["path"] != "/requests" || entry["status"] != float64(201) || entry["bytes"] != float64(2) || entry["hijacked"] != false || entry["aborted"] != false {
		t.Fatalf("incorrect request metadata: %v", entry)
	}

	if duration, ok := entry["duration"].(float64); !ok || duration <= 0 {
		t.Fatal("request duration was not recorded")
	}

	for _, secret := range []string{"query-secret", "header-secret", "private-operation-key", "0123454821", "private-prompt"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("request content leaked into logs: %s", secret)
		}
	}
}

func TestRequestLogsCaptureImplicitStatusAndUnmatchedRoutes(t *testing.T) {
	for _, unmatched := range []bool{false, true} {
		t.Run(map[bool]string{false: "implicit-200", true: "unmatched-404"}[unmatched], func(t *testing.T) {
			output := captureRequestLogs(t)
			mux := http.NewServeMux()
			mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {})
			path := "/health"
			want := http.StatusOK

			if unmatched {
				path = "/missing"
				want = http.StatusNotFound
			}

			r := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			logRequests(mux).ServeHTTP(w, r)
			entry := requestLog(t, output)

			if entry["status"] != float64(want) || w.Code != want {
				t.Fatalf("incorrect response status: log=%v HTTP=%d", entry, w.Code)
			}
		})
	}
}

func TestRequestLogsPreserveHijackAndReportNoHTTPResponse(t *testing.T) {
	output := captureRequestLogs(t)
	logged := make(chan struct{})
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()

		if err != nil {
			t.Error(err)

			return
		}

		_ = connection.Close()
	}))
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(logged)

		h.ServeHTTP(w, r)
	}))

	defer s.Close()

	client := s.Client()
	client.Timeout = time.Second
	res, err := client.Get(s.URL)

	if err == nil {
		res.Body.Close()
		t.Fatal("hijacked connection incorrectly returned an HTTP response")
	}

	select {
	case <-logged:
	case <-time.After(time.Second):
		t.Fatal("lost response request was not logged")
	}

	entry := requestLog(t, output)

	if entry["hijacked"] != true || entry["status"] != float64(0) || entry["bytes"] != float64(0) || entry["aborted"] != false {
		t.Fatalf("lost response was logged as a normal HTTP response: %v", entry)
	}
}

func TestRequestLogsRecordAndPropagatePanics(t *testing.T) {
	output := captureRequestLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("handler failure")
	}))
	panicked := false

	func() {
		defer func() { panicked = recover() == "handler failure" }()

		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/requests", nil))
	}()

	entry := requestLog(t, output)

	if !panicked || entry["aborted"] != true || entry["status"] != float64(500) {
		t.Fatalf("panic was swallowed or logged as success: %v", entry)
	}
}
