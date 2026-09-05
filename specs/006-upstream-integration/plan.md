# Integration plan

1. Freeze both commits and record local deployment-file hashes. Work in
   `.worktrees/integrate-upstream-20260905` on
   `chore/integrate-upstream-20260905`.
2. Three-way merge upstream, retaining RelayDeck README content, combining
   migration definitions, and translating new owned imports/labels.
3. Adapt the new index recovery call to RelayDeck's existing
   `dropUnusableIndexIfPresent` implementation. Add regression coverage for
   healthy/absent versus invalid/unready indexes.
4. Verify generated Ent group metadata and both local and upstream features.
   Use pnpm 9 from CI with the unchanged frozen lockfile.
5. Run integration and migration checks on disposable PostgreSQL/Redis
   instances, record results, and obtain an independent read-only code review.
6. Create a merge commit, fast-forward local main when still based on the
   recorded commit, and recheck ancestry and deployment-file hashes.

## Rollback

Before source integration, main remains at the baseline. After the local merge,
reverting the merge restores the previous source behavior. No production
database changes are performed by this workflow.
