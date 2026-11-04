#!/usr/bin/env python3
"""Create reviewable teaching branches from committed main, without checkout.

Existing branches are left alone. Run after committing the completed reference.
"""
import os
from pathlib import Path
import re
import subprocess
import tempfile

def git(*args, data=None, env=None):
    return subprocess.check_output(["git", *args], input=data, env=env).decode().strip()

main = git("rev-parse", "main")
initial = git("rev-list", "--max-parents=0", "main").splitlines()[0]

def source(path): return git("show", main + ":" + path) + "\n"

def replace_block(text, marker, replacement):
    start = text.index(marker)
    brace = text.index("{", start)
    depth = 1
    end = brace + 1
    while depth:
        if text[end] == "{": depth += 1
        if text[end] == "}": depth -= 1
        end += 1
    return text[:start] + replacement + text[end:]

def create(name, parent, changes, message):
    if subprocess.run(["git", "show-ref", "--verify", "--quiet", "refs/heads/" + name]).returncode == 0:
        print("Preserved existing", name)
        return git("rev-parse", name)
    with tempfile.TemporaryDirectory(prefix="devfest-index-") as tmp:
        env = dict(os.environ, GIT_INDEX_FILE=str(Path(tmp) / "index"))
        git("read-tree", main, env=env)
        for path, text in changes.items():
            blob = git("hash-object", "-w", "--stdin", data=text.encode())
            git("update-index", "--add", "--cacheinfo", "100644," + blob + "," + path, env=env)
        tree = git("write-tree", env=env)
        commit = git("commit-tree", tree, "-p", parent, data=(message + "\n").encode())
        git("update-ref", "refs/heads/" + name, commit)
        print("Created", name, commit[:8])
        return commit

names = ["00-ready", "01-read-tools", "02-beneficiary-exercise", "03-beneficiary-solution", "04-approved-payments", "05-recovery"]
parent = initial
for stage, name in enumerate(names):
    changes = {"internal/agent/checkpoint.go": source("internal/agent/checkpoint.go").replace("const Stage = 5", "const Stage = " + str(stage))}
    if stage == 2:
        tools = replace_block(source("internal/agent/tools.go"), '\tif Stage == 2 {', '')
        changes["internal/agent/tools.go"] = tools
        changes["internal/agent/beneficiary.go"] = '''package agent

import (
    "context"
    "devfest26/internal/domain"
)

// EXERCISE: validate, call the supplied service client with the stable key,
// and return the saved beneficiary. See docs/beneficiary-exercise.md.
func (r *Router) AddBeneficiary(ctx context.Context, key string, in domain.AddBeneficiary) (map[string]any, error) {
    return nil, domain.Invalid("exercise: implement add_beneficiary")
}
'''
    parent = create("workshop/" + name, parent, changes, "workshop: " + name)

for lab, flag, marker, stub in [
    ("01-notifications", "NotificationsEnabled", '\tif NotificationsEnabled && result.Status == "succeeded" {', '\tif NotificationsEnabled && result.Status == "succeeded" {\n\t\ts.Error = "Exercise: schedule DeliverReceipt without repeating payment"\n\t}'),
    ("02-scheduled-transfers", "ScheduleEnabled", '\tif ScheduleEnabled && s.Proposal.ExecuteAt != "" {', '\tif ScheduleEnabled && s.Proposal.ExecuteAt != "" {\n\t\ts.Phase = "error"\n\t\ts.Error = "Exercise: add the durable scheduled wait before payment"\n\t\treturn s, nil\n\t}'),
]:
    checkpoint = source("internal/agent/checkpoint.go").replace("const " + flag + " = false", "const " + flag + " = true")
    start = create("practice/" + lab + "-start", main, {"internal/agent/checkpoint.go": checkpoint, "internal/agent/workflow.go": replace_block(source("internal/agent/workflow.go"), marker, stub)}, "practice: " + lab + " starter")
    create("practice/" + lab + "-solution", start, {"internal/agent/checkpoint.go": checkpoint}, "practice: " + lab + " completed solution")
