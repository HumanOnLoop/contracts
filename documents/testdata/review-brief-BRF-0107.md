---
id: BRF-0107
run: RUN-0042-1
intent: INT-0042 v1
category: implementation
to: Developer, Orders team
raised_by: implementer
urgency: normal
deadline: 2026-11-16T15:00
default: A
blocks: [T-7, T-8]
---

## Question
Should large CSV exports read from the read replica or from the primary database?

## Context
Criterion 2 requires async exports above 50,000 orders. The intent's constraint says exports must not slow down order placement. Branch run/RUN-0042-1 in orders-service has the async job skeleton; trace linked in the portal.

## Options
### A: Read replica (recommended)
Export queries run against the replica. Impact: no load on order placement; data can lag the primary by a few seconds.

### B: Primary, throttled
Export queries run on the primary in small batches. Impact: always current data; adds load on the primary and a throttling setting to maintain.

## Recommendation
A. It meets the constraint without tuning, and a few seconds of lag is acceptable for a date-range export.

## Precedents
None found for exports. The PDF invoice export uses the primary but handles one order at a time.

## Answer
A. Note the possible lag in the export's help text.
