package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Serve runs a loopback-only workshop HTTP service until its context stops.
func Serve(ctx context.Context, address string, h http.Handler) error {
	host, _, err := net.SplitHostPort(address)

	if err != nil || !(host == "127.0.0.1" || host == "localhost" || host == "::1") {
		return fmt.Errorf("workshop services must bind to localhost")
	}

	s := &http.Server{
		Addr:              address,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		defer cancel()

		_ = s.Shutdown(c)
	}()

	slog.Info("HTTP service ready", "address", address)

	if err = s.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	return nil
}
