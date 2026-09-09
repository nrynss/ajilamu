---
title: Cost & Economics
description: Nanodollar precision accounting, rate cards, and itemized attempt billing.
template: doc
---

Ajilamu tracks every cloud expense with exact precision. The accounting engine prevents hidden subscription bloat by pricing operations in nanodollars ($10^{-9}$ USD).

---

## Nanodollar Precision

Small API transactions frequently round to zero in conventional monitoring dashboards. Micro-costs accumulate across hundreds of takes and conceal actual production expenditures.

The accounting logic in `internal/cost/cost.go` calculates charges using integer nanodollars. A fraction of a cent registers on the interface and persists in the ClickHouse ledger.

---

## Published Rate Cards

Ajilamu applies the official provider rate cards for cloud model calls:

| Resource | Unit Measurement | Cost (USD) |
| :--- | :--- | :--- |
| **Chirp 3 HD Synthesis** | 1,000,000 characters | $30.00 |
| **Gemini 3.8 Flash Input** | 1,000,000 prompt tokens | $0.15 |
| **Gemini 3.8 Flash Output** | 1,000,000 completion tokens | $0.60 |

ffmpeg operations run entirely on local CPU cycles. The server logs zero external fee for audio stretching and muxing.

---

## The NASA Baseline Measurement

The project measures baseline performance using a committed 75-second NASA sample video dubbed into Malayalam.

| Workload | Measured Realized Cost |
| :--- | :--- |
| **75.0s Video (Malayalam)** | **$0.023414** |

This total covers video segmentation, constrained translation, speech synthesis, and verification reads across all eight dialogue segments.

---

## Itemized Attempt Transparency

Traditional platforms conceal rejected generation retries within flat platform fees.

Ajilamu records every attempt in `charges_raw`. If Gemini rewrites a phrase twice before achieving a satisfactory acoustic fit, the ledger stores three discrete charge rows. Creators observe the exact cost of each revision.
