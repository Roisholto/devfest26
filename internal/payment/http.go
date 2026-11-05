package payment

import (
	"net/http"
	"strconv"

	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

func Handler(s *Store) http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Pool.Ping(r.Context()); err != nil {
			httpx.Error(w, err)

			return
		}

		httpx.JSON(w, 200, map[string]string{"status": "ready"})
	})
	m.HandleFunc("GET /beneficiaries", func(w http.ResponseWriter, r *http.Request) {
		b, err := s.List(r.Context(), r.URL.Query().Get("q"))

		if err != nil {
			httpx.Error(w, err)

			return
		}

		httpx.JSON(w, 200, b)
	})
	m.HandleFunc("POST /beneficiaries", func(w http.ResponseWriter, r *http.Request) {
		var in domain.AddBeneficiary

		if err := httpx.Decode(w, r, &in); err != nil {
			httpx.Error(w, err)

			return
		}

		b, err := s.Add(r.Context(), r.Header.Get("Idempotency-Key"), in)

		if err != nil {
			httpx.Error(w, err)

			return
		}

		httpx.JSON(w, 200, b)
	})
	m.HandleFunc("GET /balance", func(w http.ResponseWriter, r *http.Request) {
		n, err := s.Balance(r.Context())

		if err != nil {
			httpx.Error(w, err)

			return
		}

		httpx.JSON(w, 200, map[string]any{"balance_kobo": n, "currency": "NGN"})
	})
	m.HandleFunc("POST /transfers", func(w http.ResponseWriter, r *http.Request) {
		var in domain.SubmitTransfer

		if err := httpx.Decode(w, r, &in); err != nil {
			httpx.Error(w, err)

			return
		}

		t, drop, err := s.Submit(r.Context(), in)

		if err != nil {
			httpx.Error(w, err)

			return
		}

		if drop { // Close the actual TCP connection after the database has committed.
			if h, ok := w.(http.Hijacker); ok {
				c, _, err := h.Hijack()

				if err == nil {
					_ = c.Close()

					return
				}
			}

			httpx.Error(w, &domain.Problem{
				Code:    "unavailable",
				Message: "simulated lost response after commit",
			})

			return
		}

		httpx.JSON(w, 200, t)
	})
	m.HandleFunc("GET /transfers/last", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.Last(r.Context())

		if err != nil {
			httpx.Error(w, err)

			return
		}

		httpx.JSON(w, 200, t)
	})
	m.HandleFunc("GET /transfers/{key}", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.Get(r.Context(), r.PathValue("key"))

		if err != nil {
			httpx.Error(w, err)

			return
		}

		httpx.JSON(w, 200, t)
	})
	m.HandleFunc("GET /evidence", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Evidence(r.Context())

		if err != nil {
			httpx.Error(w, err)

			return
		}

		httpx.JSON(w, 200, v)
	})
	m.HandleFunc("POST /receipts/{key}", func(w http.ResponseWriter, r *http.Request) {
		failFirst, _ := strconv.Atoi(r.URL.Query().Get("fail_first"))

		if failFirst < 0 || failFirst > 2 {
			httpx.Error(w, domain.Invalid("fail_first must be 0–2"))

			return
		}

		if err := s.Receipt(r.Context(), r.PathValue("key"), failFirst); err != nil {
			httpx.Error(w, err)

			return
		}

		httpx.JSON(w, 200, map[string]string{"status": "recorded"})
	})

	return m
}
