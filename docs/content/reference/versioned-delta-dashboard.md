---
title: "V3 delta-search dashboard"
description: "Final Redis and Valkey million-keyword release-gate results."
layout: v3-delta-dashboard
---

# V3 delta-search dashboard

Final 2026-09-14 experiment: one million keywords per distribution, three
independent processes per mode on Redis 8.10.1 and Valkey 9.1.2. Each ratio
compares the median delta-search result with the same full-rebuild baseline.
The [validation report](../versioned-delta-validation/) explains the workload,
release gates, and reproduction commands.

The dashboard also includes the archived V3 baseline and R2/R3 measurements
below. They use a different benchmark schema, so their metadata is shown
separately from the final delta-search release gate.
