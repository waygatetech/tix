#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp" 2>/dev/null || true' EXIT

# build before overriding HOME so go's cache stays in the real one
go build -o "$tmp/tix" .

# isolated database so the suite never touches a real ~/.tix DB
export TIX_DB="$tmp/tix.db"

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

check_status() {
  local desc="$1" status="$2"
  shift 2
  set +e
  "$@" >/dev/null 2>&1
  local got=$?
  set -e
  if [[ "$got" -eq "$status" ]]; then
    echo "ok: $desc"
    pass=$((pass + 1))
  else
    echo "FAIL: $desc" >&2
    echo "  expected status: $status" >&2
    echo "  got status: $got" >&2
    exit 1
  fi
}

# releases stamp main.version through ldflags; an unstamped build says "dev"
check "--version reports the build" "tix version dev" "$tmp/tix" --version

# help, version, and completion never open the database, so they keep working
# when the database path cannot be created
: >"$tmp/not-a-dir"
unusable_db="$tmp/not-a-dir/tix.db"
check_status "--help works without a usable database" 0 env TIX_DB="$unusable_db" "$tmp/tix" --help
check_status "--version works without a usable database" 0 env TIX_DB="$unusable_db" "$tmp/tix" --version
check_status "completion works without a usable database" 0 env TIX_DB="$unusable_db" "$tmp/tix" completion bash
check_status "help subcommand works without a usable database" 0 env TIX_DB="$unusable_db" "$tmp/tix" help list
check "a ticket command reports the unusable database" "mkdir failed" env TIX_DB="$unusable_db" "$tmp/tix" list --project acme
check_status "a ticket command fails on the unusable database" 1 env TIX_DB="$unusable_db" "$tmp/tix" list --project acme

# projects are rows: a name has to be created before tickets can land in it,
# either up front or with --create on the first ticket
check "add --create defines project acme" "created project acme" "$tmp/tix" add --project acme "fix login bug" -b "users can't log in" --create
check "add high-priority ticket" "ac-2 [P0] write docs" "$tmp/tix" add --project acme "write docs" -b "README" -p 0
check "add ticket in a second project" "ze-3 [P2] refactor parser" "$tmp/tix" add --project zen "refactor parser" -b "split tokens" --create
printf "generated body from a file" >"$tmp/body.txt"
check "add reads body from file" "fi-4 [P1] file-backed ticket" "$tmp/tix" add --project files "file-backed ticket" --body-file "$tmp/body.txt" -p 1 --create

# an unknown project is the error that stops a typo from forking the backlog
check "add rejects an unknown project" 'unknown project "acmee"' "$tmp/tix" add --project acmee "typo ticket" -b body
check "unknown project error lists known ones" "known projects are acme, files, zen" "$tmp/tix" add --project acmee "typo ticket" -b body
check_status "add with an unknown project exits non-zero" 1 "$tmp/tix" add --project acmee "typo ticket" -b body
check "list rejects an unknown project" 'unknown project "acmee"' "$tmp/tix" list --project acmee
check_status "list with an unknown project exits non-zero" 1 "$tmp/tix" list --project acmee

# variant spellings resolve to the project that exists instead of making a new
# one, which is what agents with no shell history keep producing
check "a variant spelling lists the real project" "fix login bug" "$tmp/tix" list --project ACME
check "a punctuated variant resolves too" "fix login bug" "$tmp/tix" list --project a-c-m-e
# 'acme!' sorts after 'acme', so a prefix derived from the typed name instead of
# the resolved one would print acm-5 for a ticket that is really ac-5
check "--create on an existing variant reuses it" "ac-5 [P2] variant ticket" "$tmp/tix" add --project 'acme!' "variant ticket" -b body --create
check_absent "the variant did not create a second project" "Acme (" "$tmp/tix" projects
check "rm removes the variant ticket" "ac-5 removed" "$tmp/tix" rm 5 --project acme

check "list shows all acme tickets" "write docs" "$tmp/tix" list --project acme
check "list acme also shows #1" "fix login bug" "$tmp/tix" list --project acme
check_absent "list acme excludes zen ticket" "refactor parser" "$tmp/tix" list --project acme
check "list supports JSON output" '"title": "write docs"' "$tmp/tix" list --project acme --json
check "list supports compact JSON output" '[{"id":2,' "$tmp/tix" list --project acme --json --compact
check "list filters by status" '"status":"open"' "$tmp/tix" list --project acme --status open --json --compact
check "list filters by priority" '"priority":0' "$tmp/tix" list --project acme --priority 0 --json --compact
check "list limits results" '[{"id":2,' "$tmp/tix" list --project acme --limit 1 --json --compact
check "list supports JSON field selection" '[{"id":2,"title":"write docs"}' "$tmp/tix" list --project acme --limit 1 --json --compact --fields id,title

# search is the duplicate check: it looks in the body as well as the title, so
# a ticket whose subject only appears in its description still turns up
check "search matches a title" "fix login bug" "$tmp/tix" search --project acme login
check "search matches a body" "write docs" "$tmp/tix" search --project acme README
check "search ignores case" "fix login bug" "$tmp/tix" search --project acme LOGIN
check "search joins unquoted words into one term" "fix login bug" "$tmp/tix" search --project acme fix login
check_absent "search stays inside the project" "refactor parser" "$tmp/tix" search --project acme parser
check_absent "search treats wildcards literally" "write docs" "$tmp/tix" search --project acme '%'
check "search supports JSON field selection" '[{"id":2,"title":"write docs"}' "$tmp/tix" search --project acme docs --json --compact --fields id,title
check "search requires a term" "search term is required" "$tmp/tix" search --project acme
check_status "search without a term exits non-zero" 1 "$tmp/tix" search --project acme
check "search rejects an unknown project" 'unknown project "acmee"' "$tmp/tix" search --project acmee login

check "projects lists acme counts" "acme (ac) open=2 total=2" "$tmp/tix" projects
check "projects lists files counts" "files (fi) open=1 total=1" "$tmp/tix" projects
check "projects lists zen counts" "zen (ze) open=1 total=1" "$tmp/tix" projects
check "projects supports JSON output" '"status_counts": {' "$tmp/tix" projects --json
check "projects supports compact JSON output" '[{"project":"acme",' "$tmp/tix" projects --json --compact
check "projects supports JSON field selection" '[{"project":"acme","total":2}' "$tmp/tix" projects --json --compact --fields project,total

# projects can also be created up front, with the ID prefix and repo path the
# rest of tix reads back
check "projects add creates a project" "created project sandbox (sbx)" "$tmp/tix" projects add sandbox --key sbx --repo-path /tmp/sandbox
check "projects lists a project with no tickets" "sandbox (sbx)" "$tmp/tix" projects
check "projects reports the repo path in JSON" '"repo_path":"/tmp/sandbox"' "$tmp/tix" projects --json --compact
check "projects add rejects a duplicate" 'project "sandbox" already exists' "$tmp/tix" projects add sandbox
check "projects add rejects a variant of an existing project" 'already exists as "sandbox"' "$tmp/tix" projects add Sand-Box
check_status "projects add with a duplicate exits non-zero" 1 "$tmp/tix" projects add SANDBOX
check_status "listing an empty project succeeds" 0 "$tmp/tix" list --project sandbox
check "a pinned key prefixes the project's ticket ids" "sbx-6 [P2] sandbox ticket" "$tmp/tix" add --project sandbox "sandbox ticket" -b body
check "best-practices shows agent loop" "claim-next --project NAME ->" "$tmp/tix" best-practices
check "best-practices steers agents to plain text" "Output: plain text is the default" "$tmp/tix" best-practices
check "agent-guide alias works" "Use: TIX_DB=.tix/tix.db tix" "$tmp/tix" agent-guide
check "best-practices supports compact JSON" '{"use":"TIX_DB=.tix/tix.db tix",' "$tmp/tix" best-practices --json --compact
check "best-practices supports JSON field selection" '{"use":"TIX_DB=.tix/tix.db tix"}' "$tmp/tix" best-practices --json --compact --fields use
pushd "$tmp" >/dev/null
printf "# Existing\n" > AGENTS.md
init_out=$("$tmp/tix" init-agent acme)
if grep -qF "updated $tmp/AGENTS.md" <<<"$init_out" && grep -qF "created $tmp/CLAUDE.md" <<<"$init_out"; then
  echo "ok: init-agent updates AGENTS and creates CLAUDE"
  pass=$((pass + 1))
else
  echo "FAIL: init-agent updates AGENTS and creates CLAUDE" >&2
  echo "$init_out" >&2
  exit 1
fi
check "init-agent is idempotent" "unchanged $tmp/AGENTS.md" "$tmp/tix" init-agent acme
for pair in "tix claim-next --project acme\`:AGENTS.md" "# Existing:AGENTS.md" "TIX_DB=.tix/tix.db tix:CLAUDE.md"; do
  needle="${pair%%:*}"
  file="${pair##*:}"
  if grep -qF -- "$needle" "$file"; then
    echo "ok: $file contains '$needle'"
    pass=$((pass + 1))
  else
    echo "FAIL: $file missing '$needle'" >&2
    echo "  --- $file contents ---" >&2
    cat "$file" >&2
    exit 1
  fi
done
popd >/dev/null

mkdir -p "$tmp/git-project/nested"
git -C "$tmp/git-project" init -q
pushd "$tmp/git-project/nested" >/dev/null
check "init-agent finds Git repository root" "/git-project/AGENTS.md" "$tmp/tix" init-agent acme
if [[ -f "$tmp/git-project/CLAUDE.md" && ! -f "$tmp/git-project/nested/CLAUDE.md" ]]; then
  echo "ok: init-agent writes guidance at Git root"
  pass=$((pass + 1))
else
  echo "FAIL: init-agent writes guidance at Git root" >&2
  exit 1
fi
popd >/dev/null

check "next returns highest priority first" "write docs" "$tmp/tix" next --project acme
check "next supports JSON output" '"id": 2' "$tmp/tix" next --project acme --json
check "next supports compact JSON output" '{"id":2,' "$tmp/tix" next --project acme --json --compact
check "next supports JSON field selection" '{"id":2,"title":"write docs"}' "$tmp/tix" next --project acme --json --compact --fields id,title
check "show displays full ticket details" "README" "$tmp/tix" show 2
check "show supports compact JSON output" '{"id":2,' "$tmp/tix" show 2 --json --compact
check "show supports JSON field selection" '{"id":2,"status":"open"}' "$tmp/tix" show 2 --json --compact --fields id,status
check "claim-next marks ticket in progress" '"status":"in_progress"' "$tmp/tix" claim-next --project zen --json --compact
check "claim-next removes claimed ticket from next" "no open tickets" "$tmp/tix" next --project zen
check "bump #1 to priority 0" "ac-1 -> P0" "$tmp/tix" bump 1 0
check "next now returns bumped ticket" "fix login bug" "$tmp/tix" next --project acme

# prefixed ids: the bare integer stays valid so existing shell history and
# scripts keep working, while the prefixed form additionally checks the project
check "show accepts a prefixed id" "ac-1 [P0] fix login bug" "$tmp/tix" show ac-1
check "show accepts a bare id" "ac-1 [P0] fix login bug" "$tmp/tix" show 1
check "show keeps integer ids in JSON" '{"id":1,' "$tmp/tix" show ac-1 --json --compact
check "prefixed id from the wrong project is rejected" 'belongs to project "acme"' "$tmp/tix" show ze-1
check_status "wrong-project id exits non-zero" 1 "$tmp/tix" show ze-1
check "prefix lists derived prefixes" "acme ac" "$tmp/tix" prefix
check "prefix reports one project" "zen ze" "$tmp/tix" prefix zen

# a bare id names no project, so mutations say which backlog they touched and
# an explicit --project must agree with the ticket instead of being ignored
check "show accepts a matching --project" "fix login bug" "$tmp/tix" show 1 --project acme
check "show rejects a mismatched --project" 'belongs to project "acme"' "$tmp/tix" show 1 --project zen
check_status "mismatched --project exits non-zero" 1 "$tmp/tix" show 1 --project zen
check "a variant spelling still matches" "fix login bug" "$tmp/tix" show 1 --project ACME
check "mismatched --project rejects an unknown project" 'unknown project "acmee"' "$tmp/tix" show 1 --project acmee
check "bump names the project it touched" "ac-1 -> P0 (acme: fix login bug)" "$tmp/tix" bump 1 0 --project acme
check_status "bump rejects a mismatched --project" 1 "$tmp/tix" bump 1 3 --project zen
check "bump left the priority alone after rejecting" "ac-1 [P0]" "$tmp/tix" show 1
check_status "done rejects a mismatched --project" 1 "$tmp/tix" done 1 --project zen
check_absent "done left the ticket open after rejecting" "x ac-1" "$tmp/tix" list --project acme -a

# edit's --project moves a ticket rather than scoping the lookup, so the move
# itself has to show up in the confirmation
movable=$("$tmp/tix" add --project acme "movable ticket" -b body | sed 's/^[a-z0-9]*-\([0-9]*\) .*/\1/')
check "edit reports a cross-project move" "moved from acme to zen" "$tmp/tix" edit "$movable" --project zen
check "the moved ticket lists under zen" "movable ticket" "$tmp/tix" list --project zen
check_absent "edit stays quiet when the project is unchanged" "moved from" "$tmp/tix" edit "$movable" --title "renamed ticket"
"$tmp/tix" rm "$movable" --project zen >/dev/null

check "rm removes a ticket" "ac-2 removed (acme: write docs)" "$tmp/tix" rm 2 --project acme
check "done marks a ticket" "ac-1 done (acme: fix login bug)" "$tmp/tix" done 1
check_absent "list excludes done ticket" "ac-1" "$tmp/tix" list --project acme
check "list --all shows done ticket" "x ac-1" "$tmp/tix" list --project acme -a
# work that already shipped is still a duplicate, so search sees closed tickets
# without being asked
check "search finds a done ticket" "x ac-1" "$tmp/tix" search --project acme login
check_absent "search narrows to a status on request" "ac-1" "$tmp/tix" search --project acme login --status open
check "projects counts done tickets" "acme (ac) done=1 total=1" "$tmp/tix" projects
check "next has no open tickets left" "no open tickets" "$tmp/tix" next --project acme

check "claimed ticket appears with all tickets" "refactor parser" "$tmp/tix" list --project zen --all

# concurrency: parallel agents share one database file, so every process must
# get through instead of failing with SQLITE_BUSY, and no ticket may be claimed
# twice
parallel=8
"$tmp/tix" projects add par >/dev/null
for i in $(seq 1 "$parallel"); do
  "$tmp/tix" add --project par "parallel ticket $i" --body "body $i" >/dev/null
done

for i in $(seq 1 "$parallel"); do
  "$tmp/tix" claim-next --project par --json --compact --fields id >"$tmp/claim.$i.out" 2>&1 &
done
wait

claimed=$(cat "$tmp"/claim.*.out)
distinct=$(sort -u <<<"$claimed" | grep -c '^{"id":[0-9]*}$' || true)
if [[ "$distinct" -eq "$parallel" ]]; then
  echo "ok: $parallel parallel claims each returned a distinct ticket"
  pass=$((pass + 1))
else
  echo "FAIL: $parallel parallel claims each returned a distinct ticket" >&2
  echo "  distinct successful claims: $distinct" >&2
  echo "  got:" >&2
  echo "$claimed" >&2
  exit 1
fi

for i in $(seq 1 "$parallel"); do
  "$tmp/tix" add --project par "parallel add $i" --body "body" >"$tmp/add.$i.out" 2>&1 &
done
wait

# parallel agents all passing --create race to create the same project; exactly
# one may win, and none may fail
for i in $(seq 1 "$parallel"); do
  "$tmp/tix" add --project racer "racing add $i" --body "body" --create >"$tmp/race.$i.out" 2>&1 &
done
wait

raced=$(cat "$tmp"/race.*.out)
raced_ok=$(grep -c '^ra-[0-9]* ' <<<"$raced" || true)
raced_created=$(grep -c '^created project racer$' <<<"$raced" || true)
if [[ "$raced_ok" -eq "$parallel" && "$raced_created" -eq 1 ]]; then
  echo "ok: $parallel parallel --create adds made one project and $parallel tickets"
  pass=$((pass + 1))
else
  echo "FAIL: $parallel parallel --create adds made one project and $parallel tickets" >&2
  echo "  successful adds: $raced_ok (want $parallel), projects created: $raced_created (want 1)" >&2
  echo "$raced" >&2
  exit 1
fi
check "the race created exactly one project" "racer (ra) open=8 total=8" "$tmp/tix" projects

added=$(cat "$tmp"/add.*.out)
added_ok=$(grep -c '^pa-[0-9]* ' <<<"$added" || true)
if [[ "$added_ok" -eq "$parallel" ]]; then
  echo "ok: $parallel parallel adds all succeeded"
  pass=$((pass + 1))
else
  echo "FAIL: $parallel parallel adds all succeeded" >&2
  echo "  successful adds: $added_ok" >&2
  echo "  got:" >&2
  echo "$added" >&2
  exit 1
fi

check_absent "parallel commands never report a locked database" "database is locked" cat "$tmp"/claim.*.out "$tmp"/add.*.out

# mcp: the server ships on the tix binary itself, so a release install can run it
check "mcp subcommand is registered" "Run an MCP server" "$tmp/tix" --help

mcp_in="$tmp/mcp.in"
mcp_out="$tmp/mcp.out"
mkfifo "$mcp_in"
: >"$mcp_out"
{
  printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
  # hold the pipe open until the reply lands; on EOF the server shuts down
  # without answering
  for _ in $(seq 1 50); do
    grep -qF '"tix_projects"' "$mcp_out" && break
    sleep 0.1
  done
} >"$mcp_in" &
"$tmp/tix" mcp <"$mcp_in" >"$mcp_out" 2>&1
wait

check "mcp answers initialize over stdio" '"serverInfo":{"name":"tix"' cat "$mcp_out"
# the version comes off the binary, so an unstamped build reports "dev" here
# rather than a number written into the server that release tags would outrun
check "mcp reports the binary's version" '"version":"dev"' cat "$mcp_out"
check "mcp advertises ticket tools" '"name":"tix_projects"' cat "$mcp_out"

# the MCP surface is the authoring set only; claiming and closing stay on the
# CLI so a client cannot close tickets it never worked on
for cut in tix_claim_next tix_done tix_rm tix_next tix_bump tix_project_add; do
  check_absent "mcp does not advertise $cut" "\"name\":\"$cut\"" cat "$mcp_out"
done

# a recorded repo path makes --project unnecessary inside that checkout, which
# is the common case now that one database holds every repo's backlog
repo="$tmp/repo"
mkdir -p "$repo/internal/deep"
check "projects add records a repo path" "created project inferred (inf)" "$tmp/tix" projects add inferred --key inf --repo-path "$repo"
check "add infers the project from the working directory" "[P2] inferred ticket" bash -c "cd '$repo/internal/deep' && '$tmp/tix' add 'inferred ticket' -b body"
check "the inferred ticket landed in the registered project" "inf-" bash -c "cd '$repo' && '$tmp/tix' list"
check "next infers the project too" "inferred ticket" bash -c "cd '$repo/internal/deep' && '$tmp/tix' next"
check "claim-next infers the project too" '"project":"inferred"' bash -c "cd '$repo' && '$tmp/tix' claim-next --json --compact"
check "an explicit --project overrides the directory" "sandbox ticket" bash -c "cd '$repo' && '$tmp/tix' list --project sandbox"
check "set-repo-path registers a project created earlier" "zen $tmp/zen-repo" bash -c "mkdir -p '$tmp/zen-repo' && '$tmp/tix' projects set-repo-path zen '$tmp/zen-repo'"
check "a project registered later infers too" "refactor parser" bash -c "cd '$tmp/zen-repo' && '$tmp/tix' list --all"

# web: the dashboard is the one view that needs every project at once, and its
# page is a template the compiler cannot check, so serve it for real and look
web_port=$((20000 + RANDOM % 20000))
"$tmp/tix" web --port "$web_port" >"$tmp/web.log" 2>&1 &
web_pid=$!
trap 'kill "$web_pid" 2>/dev/null || true; rm -rf "$tmp" 2>/dev/null || true' EXIT

web_up=""
for _ in $(seq 1 50); do
  if curl -fsS "http://localhost:$web_port/" >"$tmp/web.html" 2>/dev/null; then
    web_up=yes
    break
  fi
  sleep 0.1
done
if [[ -z "$web_up" ]]; then
  echo "FAIL: web server never came up on port $web_port" >&2
  cat "$tmp/web.log" >&2
  exit 1
fi

check "web serves a page" "<title>tix" cat "$tmp/web.html"
check "web shows tickets from one project" "refactor parser" cat "$tmp/web.html"
check "web shows tickets from another project in the same page" "sandbox ticket" cat "$tmp/web.html"
check "web displays prefixed ticket ids" "inf-" cat "$tmp/web.html"
check "web shows where a project's code lives" "$repo" cat "$tmp/web.html"
check "web api serves the same view as json" '"status_counts"' curl -fsS "http://localhost:$web_port/api/dashboard"
check "web api includes prefixed refs" '"ref"' curl -fsS "http://localhost:$web_port/api/dashboard"
check_status "web 404s on an unknown path" 22 curl -fsS "http://localhost:$web_port/nope"

kill "$web_pid" 2>/dev/null || true
wait "$web_pid" 2>/dev/null || true
trap 'rm -rf "$tmp" 2>/dev/null || true' EXIT

# graceful failures
check_status "init-agent without project exits non-zero" 1 "$tmp/tix" init-agent
check "list outside a registered repo still needs --project" "no project's repo path contains" "$tmp/tix" list
check_status "list without --project exits non-zero" 1 "$tmp/tix" list
check "rm outside a registered repo still needs --project" "no project's repo path contains" "$tmp/tix" rm 3
check_status "rm without --project exits non-zero" 1 "$tmp/tix" rm 3
check "rm requires an id" "ticket id is required" "$tmp/tix" rm --project zen
check_status "rm without id exits non-zero" 1 "$tmp/tix" rm --project zen
check "show rejects missing ticket" "no such ticket: 99" "$tmp/tix" show 99
check_status "show missing ticket exits non-zero" 1 "$tmp/tix" show 99
check "add requires a title" "ticket title is required" "$tmp/tix" add --project acme
check_status "add without title exits non-zero" 1 "$tmp/tix" add --project acme
check "add rejects both body inputs" "use either --body or --body-file" "$tmp/tix" add --project acme "bad body" --body inline --body-file "$tmp/body.txt"
check_status "add with both body inputs exits non-zero" 1 "$tmp/tix" add --project acme "bad body" --body inline --body-file "$tmp/body.txt"
check "list rejects invalid priority" "invalid priority" "$tmp/tix" list --project acme --priority nope
check_status "list with invalid priority exits non-zero" 1 "$tmp/tix" list --project acme --priority nope
check "list rejects negative limit" "limit must be >= 0" "$tmp/tix" list --project acme --limit -1
check_status "list with negative limit exits non-zero" 1 "$tmp/tix" list --project acme --limit -1
check "json fields reject unknown fields" "unknown field: missing" "$tmp/tix" show 1 --json --fields missing
check_status "json fields with unknown field exits non-zero" 1 "$tmp/tix" show 1 --json --fields missing
check "next with no open tickets reports error" "no open tickets" "$tmp/tix" next --project acme
check_status "next with no open tickets exits non-zero" 1 "$tmp/tix" next --project acme

echo
echo "all $pass checks passed"
