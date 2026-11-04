#!/usr/bin/env python3
"""Replay the captured text recording without extra packages."""
import json, sys, time
path = sys.argv[1] if len(sys.argv) > 1 else "dist/fallback/presenter/recording.cast"
with open(path) as f:
    next(f)
    previous = 0
    for line in f:
        elapsed, kind, text = json.loads(line)
        time.sleep(min(max(elapsed - previous, 0), 2))
        previous = elapsed
        if kind == "o": print(text, end="", flush=True)
