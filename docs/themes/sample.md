---
title: Quarterly review
author: Ada Lovelace
date: 2026-10-06
---

# Quarterly review

Revenue grew in every region this quarter, and the new onboarding flow cut
support requests by a third. This page shows how a theme styles the most
common elements; see the [theme guide](../themes.md) for making your own.

## Highlights

- **Revenue** is up 18% on last year
- Support requests fell from 1,240 to 830
- The `v2` API is now the default for new accounts

| Region | Revenue | Change |
| :----- | ------: | -----: |
| North  | 4.2 M   | +21%   |
| South  | 3.1 M   | +12%   |
| East   | 2.7 M   | +19%   |

> [!TIP]
> Figures are preliminary until the audit closes on the 20th.

### Method

Each account is counted once, in the region of its billing address:

```sql
SELECT region, SUM(amount) AS revenue
FROM invoices
GROUP BY region;
```

> Growth without retention is a leaky bucket.

1. Close the audit
2. Publish the final figures
