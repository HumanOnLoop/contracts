---
id: INT-0042
title: Export orders as CSV
po: Product owner, Orders
developer: Developer, Orders team
repos: [orders-service, gateway]
autonomy: L3
priority: high
due: 2026-12-15
source: https://tracker.example.com/ORD-311
---

## Outcome
Shop admins can download their orders for a chosen date range as a CSV file from the admin portal.

## Acceptance criteria
- [ ] Given an admin with orders in March, when they export 1-31 March, then the file holds exactly those orders, one row each.
- [ ] Given more than 50,000 matching orders, when they export, then the file is produced asynchronously and they are notified when it is ready.
- [ ] Given a non-admin user, when they call the export endpoint, then they get 403.

## Constraints
- Exports must not slow down order placement.
- Customer email addresses are excluded from the file.

## Out of scope
Scheduled or recurring exports; formats other than CSV.

## Notes for agents
The existing PDF invoice export in orders-service follows the async pattern to reuse.
