# 6. Update Existing Bot MRs Instead of Skipping

Date: 2026-03-25

## Status
Accepted

## Context
The original pipeline behavior skipped documentation updates whenever an open bot-created MR already existed. This meant that if the MR sat unmerged for multiple scheduler cycles, subsequent code changes were never reflected in the proposed documentation, leading to stale MRs that reviewers had to manually reconcile.

## Decision
When an open bot MR exists, commit new documentation changes to the existing MR's source branch and update the MR description to reflect the latest changes. No new MR is created; the existing one is reused.

## Consequences
**Positive:**
- Documentation in open MRs stays current with the latest code changes.
- Reduces MR sprawl; reviewers deal with a single, up-to-date MR per documentation cycle.
- Simplifies the review workflow since there is always at most one active bot MR.

**Negative:**
- The MR description is overwritten on each update, losing prior description content.
- Force-pushing or amending commits on an open MR can confuse reviewers tracking incremental changes.
- Concurrent pipeline runs could race on the same branch if not properly guarded.
