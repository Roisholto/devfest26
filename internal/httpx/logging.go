package httpx

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// logRequests records response metadata without headers, query values or bodies.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &loggingResponseWriter{
			ResponseWriter: w,
		}
		completed := false

		defer func() {
			status := response.status

			if status == 0 && !response.hijacked {
				status = http.StatusOK

				if !completed {
					status = http.StatusInternalServerError
				}
			}

			slog.InfoContext(r.Context(), "HTTP request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration", time.Since(started),
				"bytes", response.bytes,
				"hijacked", response.hijacked,
				"aborted", !completed,
			)
		}()

		next.ServeHTTP(response, r)
		completed = true
	})
}

// loggingResponseWriter tracks status, written bytes and connection hijacking
// while forwarding responses to the underlying HTTP writer.
type loggingResponseWriter struct {
	http.ResponseWriter
	status   int
	bytes    int64
	hijacked bool
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)

		return
	}

	if w.status != 0 {
		return
	}

	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}

	n, err := w.ResponseWriter.Write(body)
	w.bytes += int64(n)

	return n, err
}

// Unwrap preserves access through http.ResponseController.
func (w *loggingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Hijack preserves the provider's real lost-response scenario. Status zero
// with hijacked=true means no HTTP response was sent before the connection left
// net/http's control; it must not be logged as a successful HTTP 200.
func (w *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()

	if err == nil {
		w.hijacked = true
	}

	return conn, buffer, err
}
