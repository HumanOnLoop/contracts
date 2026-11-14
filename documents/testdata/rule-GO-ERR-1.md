---
id: GO-ERR-1
title: Wrap errors with context when returning them
scope:
  repos: ["*"]
  languages: [go]
  paths: ["**/*.go"]
severity: hard
owner: architects
version: 1
evaluator: llm
status: active
---

## Rule
A function that returns an error it got from a call adds what it was doing, and wraps with `%w`.
