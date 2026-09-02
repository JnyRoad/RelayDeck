# Validation Quickstart

## Preconditions

- Use the RelayDeck worktree with dependencies already available locally.
- Do not push, publish an image, or deploy while validating this change.

## Validation Steps

1. Run the scoped case-insensitive legacy-name scan over tracked code and
   configuration files; expect zero matches.
2. Run the focused backend configuration tests and generated-client frontend
   tests; expect success.
3. Run the backend test/build commands; expect successful module resolution
   under `github.com/JnyRoad/RelayDeck`.
4. Run frontend typecheck and production build; expect successful generation.
5. Inspect `git diff --check` and the renamed tooling/deployment paths; expect
   no whitespace errors or old path references.

## Expected Limits

- Historical OpenSpec evidence, legal documents, README files, and third-party
  referral URLs can still mention the former project because they are outside
  the code-only scope.
- Docker registry publication and DNS ownership are not validated or changed.
