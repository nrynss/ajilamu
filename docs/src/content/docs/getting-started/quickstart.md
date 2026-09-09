---
title: Quickstart
description: Set up Ajilamu locally, run sample dubs, and explore offline fixture mode.
template: doc
---

This guide steps through cloning Ajilamu, configuring credentials, building the application, and running a sample dub.

---

## Prerequisites

Before building Ajilamu, install the following tools:

- **Go**: Version 1.27.1 or later.
- **Node.js**: Version 26 and npm.
- **ffmpeg and ffprobe**: Version n9.0.1 or later. Ensure your build enables `atempo`, `amix`, `aresample`, and `sidechaincompress`.
- **ClickHouse**: A running instance with tables created from `sql/schema.sql`. `deploy/clickhouse-schema.sh` creates the database, loads the schema and grants the read-only user.
- **Google Cloud Application Default Credentials**: Required for live Gemini segmentation and Chirp 3 HD audio synthesis.

---

## Configuration

Copy the example environment file:

```bash
cp .env.example .env
```

Set `GOOGLE_CLOUD_PROJECT` and your ClickHouse connection parameters in `.env`.

Authenticate your local machine through Google Cloud SDK:

```bash
gcloud auth application-default login
```

Do not configure a Gemini API key. The official Go SDK authenticates automatically through Application Default Credentials.

Required variables carry no defaults. A missing variable halts server startup and names the offending key. The repository gitignores `.env` to prevent secret leaks.

---

## Build and Run

Compile the Svelte 5 frontend and the Go backend:

```bash
# Build the web workspace
npm --prefix web ci && npm --prefix web run build

# Compile the Go server
go build -o dist/ajilamu ./cmd/ajilamu

# Start the server
./dist/ajilamu
```

The server listens on `PORT`, which defaults to 8080. Open `http://localhost:8080` in your web browser.

---

## Run a Sample Dub

Follow these steps to produce your first dubbed video:

1. Navigate to **Create a dub** at `/new`.
2. Click **Try sample video**. The server copies the committed 75-second NASA sample into an active project workspace. You may also upload your own video file.
3. Select your target language. The sample runs into Malayalam.
4. The browser redirects to `/d/<id>`. The interface streams real-time progress as Gemini segments speech, translates dialogue, and synthesizes audio.
5. Review generated takes on the timeline. Drag a boundary, reassign a speaker, or submit instructions into the command bar.
6. Click **Play Export** to review the merged video.

Every modification creates a commit in ClickHouse. The history tab displays what changed and why.

---

## Offline Fixture Mode

You can run Ajilamu without cloud credentials or network access.

When credentials remain unset, the server starts in offline mode. It loads the committed fixture project from `testdata/`.

The timeline, length bar, audio peaks, and ledger history render immediately. Live generation stays disabled, and the server log documents this state. This mode enables frontend development and interface audits anywhere.

---

## Running Verification Tests

Run the test suite before submitting pull requests:

```bash
# Run Go unit tests
go test ./...

# Typecheck and lint the Svelte 5 workspace
npm --prefix web run check

# Verify documentation consistency
python3 tools/audit_docs.py
```

The documentation audit compares project docs against `go.mod`, `.env.example`, and disk files. Exit code 1 flags documentation drift.
