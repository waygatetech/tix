#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp" 2>/dev/null || true' EXIT

# build before overriding HOME so go's cache stays in the real one
go build -o "$tmp/tix" .

# isolated HOME so the suite never touches a real ~/.tix DB
export HOME="$tmp/.home"
mkdir -p "$HOME"

pass=0
check() {
  local desc="$1" expected="$2"
  shift 2
  local out
  out=$("$@" 2>&1) || true
  if grep -qF -- "$expected" <<<"$out"; then
    echo "ok: $desc"
    pass=$((pass + 1))
  else
    echo "FAIL: $desc" >&2
    echo "  expected: $expected" >&2
    echo "  got:" >&2
    echo "$out" >&2
    exit 1
  fi
}

check_absent() {
  local desc="$1" unexpected="$2"
  shift 2
  local out
  out=$("$@" 2>&1) || true
  if grep -qF -- "$unexpected" <<<"$out"; then
    echo "FAIL: $desc" >&2
    echo "  unexpected: $unexpected" >&2
    echo "  got:" >&2
    echo "$out" >&2
    exit 1
  fi
  echo "ok: $desc"
  pass=$((pass + 1))
}

# projects are ad-hoc: no create command, the --project string scopes everything
check "add ticket (defines project acme)" "#1 [P2] fix login bug" "$tmp/tix" add --project acme "fix login bug" -b "users can't log in"
check "add high-priority ticket" "#2 [P0] write docs" "$tmp/tix" add --project acme "write docs" -b "README" -p 0
check "add ticket in a second project" "#3 [P2] refactor parser" "$tmp/tix" add --project zen "refactor parser" -b "split tokens"

check "list shows all acme tickets" "write docs" "$tmp/tix" list --project acme
check "list acme also shows #1" "fix login bug" "$tmp/tix" list --project acme
check_absent "list acme excludes zen ticket" "refactor parser" "$tmp/tix" list --project acme

check "next returns highest priority first" "write docs" "$tmp/tix" next --project acme
check "bump #1 to priority 0" "#1 -> P0" "$tmp/tix" bump 1 0
check "next now returns bumped ticket" "fix login bug" "$tmp/tix" next --project acme

check "rm removes a ticket" "#2 removed" "$tmp/tix" rm 2 --project acme
check "done marks a ticket" "#1 done" "$tmp/tix" done 1
check_absent "list excludes done ticket" "#1" "$tmp/tix" list --project acme
check "list --all shows done ticket" "x #1" "$tmp/tix" list --project acme -a
check "next has no open tickets left" "no open tickets" "$tmp/tix" next --project acme

check "other project is unaffected" "refactor parser" "$tmp/tix" list --project zen

# graceful failures
check "list requires --project" 'required flag(s) "project" not set' "$tmp/tix" list
check "rm requires --project" 'required flag(s) "project" not set' "$tmp/tix" rm 3
check "rm requires an id" "ticket id is required" "$tmp/tix" rm --project zen
check "add requires a title" "ticket title is required" "$tmp/tix" add --project acme

echo
echo "all $pass checks passed"