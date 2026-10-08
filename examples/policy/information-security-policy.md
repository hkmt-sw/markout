---
title: Information Security Policy
author: Security Office
date: 2026-09-15
version: "2.1"
classification: Internal
approved-by: Managing Director
next-review: 2027-09-15
---

# Information Security Policy

[TOC]

## 1. Purpose

This policy sets out how Example Corp protects the information it holds: its
own, its customers' and its partners'. It says what is expected of everyone
who handles that information, and who decides when the rules do not fit.

It is the top-level document of the information security management system.
Procedures and standards that go into detail refer back to it.

## 2. Scope

The policy applies to:

- all employees, contractors and temporary staff;
- every system that stores or processes company information, whether it is
  run by Example Corp or by a supplier on its behalf;
- information in any form: electronic, printed or spoken.

It does not cover the personal use of private devices that hold no company
information.

## 3. Principles

Example Corp protects three properties of its information.

Confidentiality
: Information is available only to those who are authorized to see it.

Integrity
: Information is accurate and complete, and is changed only in authorized
  ways.

Availability
: Information and the systems that hold it can be used when they are needed.

Decisions about security are based on risk: a control is put in place where
the harm it prevents outweighs what it costs[^risk].

## 4. Roles and responsibilities

| Role | Responsibilities |
| --- | --- |
| Managing Director | Approves this policy; accepts risks above the agreed threshold |
| Chief Information Security Officer | Maintains the policy; reports on its effectiveness twice a year |
| System owners | Classify the information in their systems; approve access to it |
| IT Operations | Operate the technical controls; keep systems patched and backed up |
| All staff | Follow the policy; report incidents and suspected weaknesses |

## 5. Classification of information

Every document and data set has one of four classes. The owner of the
information decides which.

| Class | Meaning | Example | Handling |
| --- | --- | --- | --- |
| Public | Meant to be published | Product brochure | No restrictions |
| Internal | For staff, harmless if it leaks | Org chart | Not shared outside the company |
| Confidential | Would cause harm if disclosed | Customer contracts | Encrypted in transit and at rest; access on a need-to-know basis |
| Restricted | Would cause serious harm | Payroll, credentials | As Confidential, plus access logged and reviewed quarterly |

> [!IMPORTANT]
> Information that has no class is treated as **Confidential** until its
> owner says otherwise.

## 6. Rules

### 6.1 Access

1. Access is granted to named people, for a stated purpose, by the owner of
   the system.
2. Accounts are personal. Sharing an account, or a password, is not allowed.
3. Multi-factor authentication is required for:
   - remote access to the company network;
   - administrative accounts;
   - every system that holds Restricted information.
4. Access is removed on the last working day of a person who leaves, and
   reviewed when a person changes role.

### 6.2 Devices

1. Company laptops and phones are encrypted and lock after five minutes
   without use.
2. Software is installed from the company catalog. Anything else needs the
   approval of IT Operations.
3. A lost or stolen device is reported **within one hour** of noticing, as
   described in the *Incident Response Procedure*.

### 6.3 Suppliers

A supplier that handles Confidential or Restricted information signs an
agreement that binds it to controls no weaker than these, and is assessed
before the contract and every two years after.

## 7. Exceptions

A rule can be set aside for a limited time when following it is not possible
or would cost more than the risk justifies. An exception:

- is requested in writing by the system owner, with the reason and the risk;
- is approved by the Chief Information Security Officer, or by the Managing
  Director if the information is Restricted;
- has an end date, and is recorded in the exception register.

## 8. Compliance

Compliance with this policy is checked by internal audit once a year.
Deliberate or repeated violations are handled under the disciplinary
procedure.

> [!NOTE]
> Reporting a mistake of your own is never a violation. The sooner an
> incident is known, the less harm it does.

## Document history

| Version | Date | Author | Change |
| --- | --- | --- | --- |
| 1.0 | 2024-03-01 | Security Office | First issue |
| 2.0 | 2025-09-10 | Security Office | Classification scheme reduced from five classes to four |
| 2.1 | 2026-09-15 | Security Office | Multi-factor authentication extended to all Restricted systems |

[^risk]: The method is described in the *Risk Assessment Methodology*. Risks
    are scored by likelihood and impact, and reviewed at least once a year.
