// Package httpx provides strict, bounded JSON at both HTTP boundaries.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"devfest26/internal/domain"
)

func Decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()

	if err := d.Decode(v); err != nil {
		return domain.Invalid("invalid JSON: " + err.Error())
	}

	if err := d.Decode(new(any)); err != io.EOF {
		return domain.Invalid("body must contain exactly one JSON value")
	}

	return nil
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func Error(w http.ResponseWriter, err error) {
	var p *domain.Problem

	if errors.As(err, &p) {
		status := 400

		switch p.Code {
		case "not_found":
			status = 404
		case "conflict":
			status = 409
		case "unavailable":
			status = 503
		}

		JSON(w, status, p)

		return
	}

	JSON(w, 503, domain.Problem{
		Code:    "unavailable",
		Message: "dependency unavailable; retry using the same operation key",
	})
}

type Client struct {
	Base string
	HTTP *http.Client
}

func NewClient(base string) *Client {
	return &Client{
		Base: base,
		HTTP: &http.Client{
			Timeout: 4 * time.Second,
		},
	}
}

func (c *Client) Do(ctx context.Context, method, path, key string, in, out any) error {
	var body io.Reader

	if in != nil {
		b, err := json.Marshal(in)

		if err != nil {
			return err
		}

		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)

	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}

	res, err := c.HTTP.Do(req)

	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.StatusCode >= 400 {
		var p domain.Problem

		if err := json.NewDecoder(io.LimitReader(res.Body, 32<<10)).Decode(&p); err != nil {
			return fmt.Errorf("HTTP %d", res.StatusCode)
		}

		return &p
	}

	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	}

	return nil
}
