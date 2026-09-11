# Blast-radius signals for tix claim-next gate

## Shared (all repos)
Ticket requires a plan-first pause if it touches any of:
- Auth or session/token handling
- Data deletion, backfill, or any irreversible write path
- Cross-repo coordination (touches something described in another repo)
- Anything DECISIONS.md marks as "ask before changing"

## Repo-specific
<!-- Each repo appends its own signals below this line when synced.
     Edit repo-specific entries in that repo's own copy after sync;
     re-syncing overwrites the Shared section only if you use the
     merge script below, or the whole file if you use the simple version. -->