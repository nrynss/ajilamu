# P3 close: independent verification, round 1

**Scope:** Phase 3 exit criteria for T3.1 through T3.5 as they stand in the tree.
**Method:** Independent measurement of every claim. No reliance on per-task review files.
**Verdict:** APPROVE. Findings: 0 C, 0 H, 0 M, 0 L.

I own only this review file. The assembler shapes exist. I can validate the phase alone.
I did not treat task review files as evidence. I re-measured every artifact.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 0 | 0 |

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| None | No findings at any severity. | Independent artifact measurements below. | Reverting duck normalize, packet copy, or placement would fail those pins. |

## Task status and owns

Every T3 task reads `status: done`. Every owns path exists on disk.

| Task | Owns | Present |
|---|---|---|
| T3.1 | `internal/assemble/bed.go`, `bed_test.go` | yes |
| T3.2 | `internal/assemble/place.go`, `place_test.go` | yes |
| T3.3 | `internal/assemble/duck.go`, `duck_test.go` | yes |
| T3.4 | `internal/assemble/export.go`, `export_test.go` | yes |
| T3.5 | `internal/assemble/peaks.go`, `peaks_test.go` | yes |

I did not tick the phase exit boxes.

## Prior-round residue

I re-measured the pins that closed T3.1 through T3.5. I did not quote those reviews as proof.

**T3.1: zero residue.** Source and bed match sample for sample outside dialogue.
Seconds 0 to 5.5 and 54.5 to 75 are identical. RMS delta is 0.000 dB.
A delayed source still keeps silence from 0 to 1.000023 s.

**T3.2: zero residue.** All eight finals match the prepared take at the segment start.
The voiced collision keeps junction energy near the body. It is not a silent overwrite.
Truncate still yields at the next start and keeps post-voice silence until then.

**T3.3: zero residue.** Duck lasts as long as the bed on 0.5 s, 1 s, 2 s, and 6 s.
The former 1 s omitted tail still holds ducked bed energy at -43.6 dB.
Gaps stay with the bed. Speech band change is 0.000 dB, not about -6 dB.

**T3.4: zero residue.** Both named exports last 75.008267 s with 4496 copied video packets.
Tail RMS stays inside 0.04 dB of the source. Names are `dubbed_replaced` and `dubbed_ducked`.

**T3.5: zero residue.** All ten takes yield 128 bins. Independent f32le buckets match the driver.
`seg_1_try1.wav` still rises through bins 9 to 13 as `50, 120, 228, 246, 255`.

No prior finding reopens. I found no additional phase defect.

## Independent measurements

I copied the production assembler and media helpers into a temporary module outside the repository.
A small driver called `BuildBed`, `Place`, `Overlay`, `Duck`, `Export`, and `Peaks`.
The driver supplied artifacts. Its success messages supplied no evidence.

Every measurement selected `/opt/homebrew/bin/ffmpeg` and `/opt/homebrew/bin/ffprobe`, both 9.0.1.
`PATH=/opt/homebrew/bin:/usr/bin:/bin`. Go reports 1.27.1.

`go test -count=1 ./internal/assemble` passed. That check does not prove the audio claims.

### Background bed outside dialogue

Decoded PCM uses `atrim` plus `pcm_f32le` at 44100 Hz stereo.

| Window (s) | Pair | Samples | Delta (dB) | Identical |
|---|---|---:|---:|---|
| 0 to 5.5 | bed vs source | 485100 | 0.000000 | yes |
| 0 to 5.5 | overlay vs source | 485100 | 0.000000 | yes |
| 0 to 5.5 | ducked vs bed | 485100 | 0.000000 | yes |
| 54.5 to 75 | bed vs source | 1808100 | 0.000000 | yes |
| 54.5 to 75 | overlay vs source | 1808100 | 0.000000 | yes |
| 54.5 to 75 | ducked vs bed | 1808100 | 0.000000 | yes |

`volumedetect` reports -13.4 dB mean on the opening window for source, bed, overlay, and duck.
It reports -15.4 dB mean on the tail for those same four files.
The speech layer is digital zero in both windows.
`silencedetect` at -90 dB covers 0 to 5.5 s and the 20.5 s tail on the speech layer.

Mid-film gaps at 35 to 38 s and 45.5 to 47.3 s also stay at 0.000 dB versus the bed.
Speech there is digital zero. Overlay matches the bed.

### Tail within 0.5 dB

| Artifact | Tail RMS | RMS (dB) | Versus source (dB) |
|---|---:|---:|---:|
| source | 0.170700318 | -15.355 | 0.000 |
| bed | 0.170700318 | -15.355 | 0.000 |
| overlay mix | 0.170700318 | -15.355 | 0.000 |
| ducked mix | 0.170700318 | -15.355 | 0.000 |
| `dubbed_replaced.mp4` | 0.169994105 | -15.391 | -0.036 |
| `dubbed_ducked.mp4` | 0.169947340 | -15.394 | -0.038 |

Both muxed tails stay inside 0.5 dB. AAC accounts for the 0.04 dB gap.
`silencedetect` at -60 dB for 1 s finds no silence in this window on source or either export.

### Ducking without speech cut

A 4 s 80 Hz bed with 2 kHz speech from 1 s to 2 s isolates the bands.

| Window (s) | Kind | Bed band (dB) | Voice band (dB) |
|---|---|---:|---:|
| 0.2 to 0.8 | gap | 0.000000 | silent |
| 2.5 to 3.5 | gap | 0.000000 | silent |
| 1.2 to 1.8 | speech | -5.309128 | 0.000027 |

Gaps stay with the bed. The bed dips under speech. Speech is not cut by about 6 dB.

Fixture dialogue uses `mix - speech` against the bed.

| Window (s) | Bed dip (dB) | Mix vs speech (dB) | Duck vs overlay (dB) |
|---|---:|---:|---:|
| 8.5 to 12.5 | -6.799 | 2.676 | -4.376 |
| 39.5 to 44.0 | -6.582 | 2.666 | -4.199 |

Mix stays above the speech layer. A 6 dB speech cut would drop that column near -6 dB.

Duck duration matches the bed on every length I measured.

| Bed (s) | Duck (s) | Overlay (s) | PCM samples |
|---|---:|---:|---:|
| 0.500000 | 0.500000 | 0.500000 | 44100 |
| 1.000000 | 1.000000 | 1.000000 | 88200 |
| 2.000000 | 2.000000 | 2.000000 | 176400 |
| 6.000000 | 6.000000 | 6.000000 | 529200 |

The 1 s window from 0.928798 s to 1.000000 s is present.
Duck there measures -31.8 dB mean. `mix - speech` measures -43.6 dB mean.
That tail holds ducked bed energy. It is not digital silence.

Creator-supplied music sets `SeparateMusic`. Duck then equals Overlay sample for sample.
Gaps and the under-speech remainder stay at 0.000 dB versus that bed.

### Placement and collision policy

`BuildBed` on `testdata/clip.mp4` yields 44100 Hz, two channels, and 75.008005 s.
The speech layer, overlay, and duck share that format and duration.

The first 80 ms of each prepared take matches the speech layer at the `segments.json` start.

| Segment | Start (s) | Policy | 80 ms SHA-1 | Mean / max (dB) |
|---|---:|---|---|---|
| 1 | 5.908 | fit | `c56ddbf2a60a7ea9be18187791739fbe443a28a9` | -46.5 / -28.6 |
| 2 | 7.728 | fit | `0139720ab241230551631f6cb90052bfe359c8b1` | -62.6 / -54.7 |
| 3 | 13.208 | gap | `39b31c86bb67cb2c076d807c28e97342126b911f` | -63.0 / -54.9 |
| 4 | 18.818 | gap | `f0970cd60df677fcc647a2f4b1422efdc6230864` | -65.8 / -58.7 |
| 5 | 24.788 | fit | `2f91c845c210bac59d1cebeffed1f9c76fe37802` | -63.8 / -55.9 |
| 6 | 30.348 | fit | `736b7dcfbd4a1a5adb651829d4f4eaa46935f1df` | -53.5 / -45.8 |
| 7 | 38.271 | fit | `4ec8ee03ddb261f8e52531dc85787ff52ceeb78c` | -55.5 / -49.2 |
| 8 | 47.381 | fit | `5ca81495b2991aa825e51c7d7fabb6a4cc7d7b65` | -63.2 / -57.0 |

The 80 ms before each later take is digital zero.
`Place` records a policy on every event. Segments 3 and 4 use Gap.

Fixture segment 3 continues past its 18.528 s slot end.
The 18.528 s to 18.546 s window measures -57.4 dB mean and -54.2 dB maximum.
Peak there is 0.001953 with 1550 nonzero values in 1588 samples.
The unused gap from 18.56 s to 18.80 s is digital zero.

A 1.2 s tone in a 1.0 s slot with the next take at 1.5 s records Gap.
Windows 0.2 to 0.6 s, 1.05 to 1.15 s, and 1.60 to 1.80 s each measure -36.1 dB mean.
The remaining gap from 1.25 s to 1.40 s is digital zero.
`silencedetect` at -90 dB places that silence from 1.200 s to 1.500023 s.

A 0.8 s tone plus 0.4 s silence in an 0.8 s slot meets the next take at 1.0 s.
The event records Truncate. Kept tone from 0.2 s to 0.6 s measures -36.1 dB.
Kept silence from 0.85 s to 0.95 s is digital zero.
`silencedetect` at -90 dB reports silence from 0.800 s to 1.000023 s.
The next take from 1.10 s to 1.30 s measures -36.1 dB.

A 1.5 s voiced tone into a 1.0 s slot with the next take at 1.0 s records Crossfade.
The fade window is 40 ms. Early body 0.2 to 0.8 s measures -36.1 dB.
Later body 1.2 to 1.6 s also measures -36.1 dB.

| Window (s) | Mean (dB) | Max (dB) |
|---|---:|---:|
| 0.990 to 1.000 | -36.048 | -33.115 |
| 1.000 to 1.010 | -36.055 | -33.115 |
| 0.995 to 1.005 | -36.219 | -33.115 |

`silencedetect=noise=-40dB:d=0.005` reports silence from 1.999841 s to 3 s only.
It does not cover 1.000 s.
The stereo frame at t=1.000000 is 0 because that phase is a zero crossing.
Neighbors are -0.001386 and +0.001381. Of 882 center samples, only that pair is zero.
This is a crossfade, not a silent overwrite.

### Export

| Artifact | Format duration (s) | Video packets | Audio rate | Channels | Layout |
|---|---:|---:|---:|---:|---|
| `testdata/clip.mp4` | 75.008267 | 4496 | 44100 | 2 | stereo |
| `dubbed_replaced.mp4` | 75.008267 | 4496 | 44100 | 2 | stereo |
| `dubbed_ducked.mp4` | 75.008267 | 4496 | 44100 | 2 | stereo |

Video codec is h264 `avc1` on all three. Packet lists match.
The dump is `pts,dts,size,flags`. SHA-256 is
`bf3ecf0bf4409844cda3669091e9eed0fca0a058c5bab963911b22e9432b5960`
on the source and on both exports.

The first three packets are `0,-2002,914,K__`, then `2002,-1001,742,___`, then `1001,0,38,___`.
Re-encoding would change those sizes.

Written names are `dubbed_replaced.mp4` and `dubbed_ducked.mp4`.
Neither name contains `clean`.

### Peaks

I decoded each take with ffmpeg 9.0.1 to packed `f32le`.
I formed frames from the probed channel count. I took max-abs per frame.
I folded frames into 128 equal-width bins. I scaled each bin to the take peak on 0 to 255.
Those bins matched the driver on every take.

| File | Duration (s) | Frames | Length | Unique | Min | Max | Match |
|---|---:|---:|---:|---:|---:|---:|---|
| seg_1_try1.wav | 1.680375 | 26886 | 128 | 77 | 0 | 255 | yes |
| seg_2_try1.wav | 5.320375 | 85126 | 128 | 88 | 0 | 255 | yes |
| seg_3_stretched.wav | 5.337563 | 85401 | 128 | 88 | 0 | 255 | yes |
| seg_3_try1.wav | 5.720375 | 91526 | 128 | 91 | 0 | 255 | yes |
| seg_4_stretched.wav | 5.662250 | 90596 | 128 | 87 | 0 | 255 | yes |
| seg_4_try1.wav | 5.920375 | 94726 | 128 | 82 | 0 | 255 | yes |
| seg_5_try1.wav | 4.720375 | 75526 | 128 | 76 | 0 | 255 | yes |
| seg_6_try1.wav | 4.440375 | 71046 | 128 | 81 | 0 | 255 | yes |
| seg_7_try1.wav | 6.800375 | 108806 | 128 | 90 | 1 | 255 | yes |
| seg_8_try1.wav | 4.200375 | 67206 | 128 | 84 | 0 | 255 | yes |

Every length sits in 64 to 128. The vectors are not flat.

## Exit criteria

| Criterion | Independent result |
|---|---|
| Background audio survives intact outside dialogue | Opening, tail, and mid-film gaps match the bed and source. |
| Tail audio within 0.5 dB of the soundtrack | Mix delta 0.000 dB. Muxed exports stay inside 0.04 dB. |
| Dynamic ducking without speech attenuation | Bed dips 5 to 7 dB. Speech band change is 0.000 dB. |
| Overrun collision policies enforced and recorded | Fit, Gap, Truncate, and Crossfade exist. Voiced collision crossfades. |
| Unit tests pass offline on fixtures | `go test -count=1 ./internal/assemble` passed. |

## Note, not a finding

`dev-diary/README.md` still prints P3 as 0 of 5 and not started.
Task lines in `PHASE-3-assembler.md` already read `done`.
That board is coordination copy. It does not change assembler behaviour.

Temporary review artifacts were removed after measurement. No production code changed during review.
