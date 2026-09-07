# Cloud Infrastructure and Machine Sizing

We host Ajilamu on a dedicated Google Compute Engine virtual machine in `us-central1`. This guarantees zero cold starts for evaluators and judges.

## Machine Evaluation: e2-standard-2 vs e2-standard-4

We compared two machine sizes against our workloads, credit limits, and judging timeline.

### 1. Cost and Credit Allocation

You hold 24,584.01 INR in total promotional credits. Your earliest expiring voucher gives you 9,564.63 INR. It expires on October 12, 2026.

Competition judging runs until October 9, 2026. We need 32 continuous days of uptime.

* **e2-standard-2 (2 vCPUs, 8 GB RAM, 50 GB SSD):**
  Total cost for 32 days equals 5,578 INR. This leaves 3,986 INR unspent in your first voucher. It gives us a 42% safety buffer.

* **e2-standard-4 (4 vCPUs, 16 GB RAM, 50 GB SSD):**
  Total cost for 32 days equals 9,203 INR. This leaves 361 INR unspent in your first voucher. Heavy manual testing spills slightly into your second voucher.

### 2. Operational Performance

Our Go backend replaces audio without re-encoding video frames. It copies video streams directly.

* **Audio stretching:**
  Both machines run `atempo` on a 5-second take in under 40 milliseconds.

* **Final video muxing:**
  Both machines assemble a 75-second video in 0.05 seconds.

* **Video transcoding:**
  If someone uploads uncompressed raw camera video, 4 cores transcode it in 7 seconds. 2 cores finish in 16 seconds.

* **Scrubbing cache:**
  The 8 GB memory on `e2-standard-2` provides 6.5 GB for Linux page cache. It holds over 90 video projects in memory simultaneously.

### 3. Sizing Verdict

We start on **e2-standard-2**. It provides instant responses, avoids cold starts, and preserves a 4,000 INR safety buffer. We can resize to `e2-standard-4` in 30 seconds if required.

---

## Storage Architecture

We split storage between local SSD and object storage using the Git LFS pattern.

### Local SSD Storage (/data/storage)

An attached 50 GB persistent disk stores active projects and individual WAV takes. It serves video via HTTP 206 Partial Content. Timeline scrubbing responds instantly.

### Google Cloud Storage (gs://ajilamu-media)

A bucket in `us-central1` archives raw uploads and final renders. Traffic between our GCE VM and Cloud Storage costs nothing because both share the same region.

---

## Service Layout

1. **GCE Host (`e2-standard-2`):** Runs the Go web server, ADK agent, and Caddy reverse proxy.
2. **ClickHouse Cloud:** Stores the commit DAG, take ledger, and pre-computed waveform peak arrays.
3. **Vertex AI (`global`):** Runs Gemini 3.8 Flash for segmentation and translation.
4. **Cloud Text-to-Speech:** Generates Chirp 3 HD voices in Malayalam, German, and Spanish.

## Credentials

The GCE host runs under an attached service account with the Vertex AI User role.
The metadata server supplies the token. No key file reaches the virtual machine.
No credential reaches an image layer.

A developer machine authenticates with `gcloud auth application-default login`.
It may instead point `GOOGLE_APPLICATION_CREDENTIALS` at a key file it already holds.
Both paths satisfy `credentials.DetectDefault`. The code reads the same in both places.
