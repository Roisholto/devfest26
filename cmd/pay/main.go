package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"devfest26/internal/agent"
	"devfest26/internal/config"
	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: pay ask|get|wait|approve|reject|transaction|reconcile|evidence [flags] [argument]")
	}

	cmd := os.Args[1]
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	key := f.String("key", "", "stable request key (printed when generated)")
	scenario := f.String("scenario", "success", "success|decline|unavailable|delayed|lost-response")
	terminal := f.Bool("terminal", false, "wait through approval/execution to a terminal outcome")
	timeout := f.Duration("timeout", 150*time.Second, "poll timeout")
	expiry := f.Int("approval-seconds", 600, "approval expiry, 1–600 seconds")

	if err := f.Parse(os.Args[2:]); err != nil {
		return err
	}

	arg := strings.Join(f.Args(), " ")

	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}

	cfg := config.Load()
	c := httpx.NewClient("http://" + cfg.API)
	ctx := context.Background()

	if cmd == "evidence" {
		var out any

		err := httpx.NewClient("http://"+cfg.Payment).Do(ctx, "GET", "/evidence", "", nil, &out)

		if err == nil {
			printJSON(out)
		}

		return err
	}

	if arg == "" {
		return fmt.Errorf("%s requires a prompt, request ID or transaction reference", cmd)
	}

	switch cmd {
	case "ask":
		if *key == "" {
			*key = domain.NewID()
		}

		var out map[string]any

		err := c.Do(ctx, "POST", "/requests", *key, map[string]any{"prompt": arg, "scenario": *scenario, "approval_seconds": *expiry}, &out)

		if err != nil {
			return err
		}

		out["request_key"] = *key
		printJSON(out)

		return nil
	case "get":
		var out agent.State

		err := c.Do(ctx, "GET", "/requests/"+arg, "", nil, &out)

		if err == nil {
			printJSON(out)
		}

		return err
	case "wait":
		deadline := time.Now().Add(*timeout)

		var lastErr error

		for time.Now().Before(deadline) {
			var out agent.State

			err := c.Do(ctx, "GET", "/requests/"+arg, "", nil, &out)

			if err == nil {
				lastErr = nil
				waiting := out.Phase == "reasoning" || out.Phase == "executing" || out.Phase == "notifying" || (*terminal && (out.Phase == "awaiting_approval" || out.Phase == "scheduled"))

				if !waiting {
					printJSON(out)

					return nil
				}
			} else {
				lastErr = err
			}

			time.Sleep(time.Second)
		}

		return fmt.Errorf("wait timed out; last dependency error: %v; request ID remains %s", lastErr, arg)
	case "approve", "reject":
		var state agent.State

		if err := c.Do(ctx, "GET", "/requests/"+arg, "", nil, &state); err != nil {
			return err
		}

		if state.Proposal == nil {
			return fmt.Errorf("request has no proposal")
		}

		d := agent.Decision{
			ProposalID: state.Proposal.ID,
			Approve:    cmd == "approve",
		}

		if *key == "" {
			*key = "decision-" + domain.Fingerprint(d)[:24]
		}

		var out any

		err := c.Do(ctx, "POST", "/requests/"+arg+"/decision", *key, d, &out)

		if err == nil {
			printJSON(out)
		}

		return err
	case "transaction", "reconcile":
		method, path := "GET", "/transactions/"+url.PathEscape(arg)

		if cmd == "reconcile" {
			method = "POST"
			path += "/reconcile"
		}

		var out any

		err := c.Do(ctx, method, path, "", nil, &out)

		if err == nil {
			printJSON(out)
		}

		return err
	}

	return fmt.Errorf("unknown command %q", cmd)
}
