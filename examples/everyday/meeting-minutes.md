# Platform team: weekly meeting

**Date:** 6 October 2026, 10:00 to 10:45
**Place:** Room 2.14 and video call
**Present:** Anna, Bence, Csilla, Dániel
**Absent:** Emese (on leave)
**Notes by:** Csilla

## 1. Actions from last week

- [x] Bence: move the staging database to the new cluster
- [x] Anna: write the runbook for password rotation
- [ ] Dániel: price comparison of the two monitoring offers *(moved to next week)*

## 2. Release 4.2

The release is ready except for one open bug: orders with more than 99 lines
are cut off in the confirmation e-mail. Bence found the cause, a field that
is too short in the template.

- The fix is small and is in review.
- Anna asked that the release wait for it, since large orders come from the
  customers who complain the loudest. Agreed.

**Decision:** release 4.2 goes out on Thursday, with the fix.

## 3. On-call rota

Dániel pointed out that the rota has two people covering all of December.
After some discussion:

1. Csilla takes the week of 14 December.
2. Whoever is on call between Christmas and New Year gets two days off in
   January, on top of the usual.
3. The rota for the first quarter is drawn up by the end of November.

## 4. Any other business

Anna showed the new dashboard for the order queue. The oldest waiting message
is now on the first page; the question was whether an alert at 30 seconds is
too sensitive. We will watch it for a week before deciding.

## Actions

| # | What | Who | By |
| ---: | --- | --- | --- |
| 1 | Merge the fix for long orders and tag release 4.2 | Bence | 8 Oct |
| 2 | Price comparison of the monitoring offers | Dániel | 13 Oct |
| 3 | Draft of the first-quarter rota | Csilla | 27 Nov |
| 4 | Report on how often the queue alert fired | Anna | 13 Oct |

**Next meeting:** 13 October 2026, 10:00, same room.
