---
title: Information Security Risk Assessment 2026
author: Security Office
date: 2026-10-05
version: "1.0"
classification: Confidential
---

# Information Security Risk Assessment 2026

[TOC]

## Executive summary

This assessment covers the systems that support order handling, billing and
customer service. It was carried out in September 2026 with the owners of
those systems.

- **14 risks** were identified, of which **2 are high**, 5 medium and 7 low.
- Both high risks concern the same weakness: backups that have not been
  tested by restoring from them.
- Treating the two high risks is estimated at 12 working days and no new
  purchases.

> [!IMPORTANT]
> The recommendation is to treat the two high risks before the end of the
> year, and to accept the low risks as they are.

## 1. Method

Each risk is given a likelihood $L$ and an impact $I$, both on a scale from
1 to 5. The score is their product:

$$
R = L \times I
$$

A score is turned into a level as follows.

| Score | Level | What is done |
| :---: | --- | --- |
| 15 to 25 | High | Treated within three months; reported to the Managing Director |
| 8 to 14 | Medium | Treated within a year, or accepted in writing by the system owner |
| 1 to 7 | Low | Accepted; looked at again at the next assessment |

### 1.1 Scales

| Value | Likelihood | Meaning |
| :---: | --- | --- |
| 1 | Rare | Less than once in ten years |
| 2 | Unlikely | Once in five to ten years |
| 3 | Possible | Once in one to five years |
| 4 | Likely | About once a year |
| 5 | Almost certain | Several times a year |

| Value | Impact | Money | Operations |
| :---: | --- | ---: | --- |
| 1 | Negligible | under € 1 000 | Not noticed by customers |
| 2 | Minor | € 1 000 to 10 000 | A service slow for an hour |
| 3 | Moderate | € 10 000 to 100 000 | A service down for a working day |
| 4 | Major | € 100 000 to 1 million | Several services down for days |
| 5 | Severe | over € 1 million | The business cannot operate |

### 1.2 Expected loss

Where figures exist, the score is checked against the expected loss in a
year. For a risk that happens on average $\lambda$ times a year and costs
$c$ each time, with a control that stops a fraction $e$ of the cases:

$$
\text{ALE} = \lambda \cdot c \cdot (1 - e)
$$

A control is worth its price when the loss it prevents in a year,
$\lambda \, c \, e$, is larger than what it costs in a year[^ale].

## 2. Risk register

The seven risks above the low level, highest first.

| # | Risk | Owner | $L$ | $I$ | Score | Level |
| ---: | --- | --- | :---: | :---: | :---: | --- |
| 1 | Backups of the order database cannot be restored | IT Operations | 4 | 5 | 20 | High |
| 2 | Backups are deleted by an attacker with admin rights | IT Operations | 3 | 5 | 15 | High |
| 3 | An account of a former employee is still active | HR, IT Operations | 4 | 3 | 12 | Medium |
| 4 | A supplier with remote access is compromised | Procurement | 3 | 4 | 12 | Medium |
| 5 | Customer data is sent to the wrong recipient | Customer Service | 4 | 3 | 12 | Medium |
| 6 | A laptop with unencrypted data is lost | IT Operations | 2 | 4 | 8 | Medium |
| 7 | The payment provider is unavailable for a day | Finance | 2 | 4 | 8 | Medium |

## 3. Findings

### 3.1 Backups are made but not tested

Backups of the order database run every night and report success. No restore
has been attempted since the system was installed in 2023. In a test during
this assessment, the restore of the most recent backup **failed**: the backup
did not include the transaction log, without which the database does not
start.

This is the largest single risk found. A failure of the database server
today would lose all orders since the installation.

### 3.2 Accounts outlive their owners

Of 212 active accounts, 9 belong to people who left the company more than a
month ago. HR informs IT Operations by e-mail, and the e-mail is sometimes
missed.

### 3.3 What works well

- All laptops issued since 2024 are encrypted; the 6 older ones are due for
  replacement in the first quarter.
- Multi-factor authentication covers every remote access path.
- Staff reported 31 suspicious e-mails in the past year, up from 12.

## 4. Recommendations

| # | Recommendation | Treats | Effort | By |
| ---: | --- | :---: | ---: | --- |
| 1 | Include the transaction log in backups; restore the database to a test server every month | 1 | 5 days | 2026-11-30 |
| 2 | Keep one backup copy where production administrators cannot delete it | 2 | 4 days | 2026-12-15 |
| 3 | Disable accounts automatically from the HR system on the last working day | 3 | 3 days | 2027-01-31 |
| 4 | Limit supplier access to named hours, with a record of each session | 4 | 6 days | 2027-03-31 |

With recommendations 1 and 2 in place, risks 1 and 2 fall to a likelihood of
2, and to a score of $2 \times 5 = 10$: medium.

## 5. Next steps

1. The Managing Director decides on the recommendations by 2026-10-20.
2. Owners report progress on accepted recommendations every month.
3. The next full assessment is due in September 2027.

[^ale]: ALE stands for annualized loss expectancy. The figures behind the
    scores in this report are in the working papers of the assessment.
