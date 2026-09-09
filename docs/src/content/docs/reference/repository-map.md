---
title: Repository Map
description: Codebase structural layout, package boundaries, and architecture invariants.
template: doc
---

This document outlines the directory structure of Ajilamu and defines the architectural boundaries between Go packages and frontend modules.

---

## Directory Organization

| Directory Path | Architectural Responsibility |
| :--- | :--- |
| `cmd/ajilamu` | Application entry point and dependency wiring. Starts HTTP listeners and initializes database connections. |
| `internal/fit` | Acoustic fit measurement, duration comparison, and the multi-step repair decision loop. |
| `internal/gemini` | Gemini API client. Handles video segmentation and duration-constrained translations. |
| `internal/tts` | Google Cloud Chirp 3 HD client, language catalogs, and speaker voice mappings. |
| `internal/assemble` | Audio layout planning, ducking calculations, and ffmpeg muxing. |
| `internal/ledger` | ClickHouse writer client and analytical projection queries. |
| `internal/agent` | ADK editor agent. Reads timeline and ledger context using MCP tools. |
| `internal/command` | Natural language parser converting user instructions into deterministic timeline mutations. |
| `internal/api` | REST endpoints, server-sent events for real-time progress, and static asset delivery. |
| `web` | Single-page editor workspace built with Svelte 5 runes and Tailwind CSS. |
| `sql/schema.sql` | ClickHouse DDL script defining tables, deduplicating views, and materialized projections. |
| `deploy/` | Automation scripts for Google Cloud provisioning, host setup, and external verification. |
| `systemd/` | Systemd service units managing the Go process and background workers. |
| `dev-diary/` | Architecture specs, phased task tracking, adversarial reviews, and validation benchmarks. |
| `testdata/` | Fixture audio takes, segmentation JSON payloads, and test video clips. |

---

## Package Invariants

The project enforces strict boundaries across modules:

### Svelte 5 Runes Only
The `web/` application uses Svelte 5 runes exclusively (`$state`, `$derived`, `$props`, `$effect`). Code must not introduce legacy Svelte 4 idioms such as `export let` or `$:`.

### Standard Library First
Go packages rely primarily on the standard library. The engine uses external packages only for official cloud SDKs (`google.golang.org/genai`) and database connectivity (`clickhouse-go`).

### ADK Isolation
The Google Agent Development Kit (`google.golang.org/adk/v2`) resides solely within `internal/agent`. No other internal package imports ADK.

### ffmpeg Exclusivity
Local ffmpeg processes handle all audio stretching, channel mixing, and video muxing. The codebase rejects third-party AI audio manipulation libraries.
