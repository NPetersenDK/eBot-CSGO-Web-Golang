#!/usr/bin/env bash
# Daily vulnerability scan. Run from the repo root; it detects what to check:
#   - go.mod                   -> govulncheck (only vulnerabilities our code can reach)
#   - package-lock.json        -> npm audit, high and above
#   - Dockerfile FROM node:N   -> is the node image behind the newest N.x release?
#   - Dockerfile runtime stage -> Trivy scan of the image that actually ships
# Writes the findings to $REPORT (default security-report.md) and exits 1 if there are any
# (or if a check could not run, which is also worth a look).
set -uo pipefail

REPORT="${REPORT:-security-report.md}"
MAX_SECTION=12000 # bytes per section, so the GitHub issue body stays under its size limit
FAIL=0
: >"$REPORT"

add() { printf '%s\n' "$*" >>"$REPORT"; }
section() { add ""; add "## $1"; add ""; }
code() { add '```'; printf '%s\n' "$1" | head -c "$MAX_SECTION" >>"$REPORT"; add ""; add '```'; }
finding() { FAIL=1; }

find_files() { find . -name "$1" -not -path '*/node_modules/*' -not -path './.git/*' | sort; }

# ---- Go -------------------------------------------------------------------
if [ -f go.mod ]; then
  section "govulncheck"
  # Packages that embed the SPA need web/dist to exist before they type-check.
  if grep -rqsE 'go:embed.*web/dist' --include='*.go' . && [ ! -d web/dist ]; then
    mkdir -p web/dist && echo placeholder >web/dist/index.html
  fi
  out=$(go run golang.org/x/vuln/cmd/govulncheck@latest ./... 2>&1)
  rc=$?
  if [ $rc -eq 0 ]; then
    add "No vulnerabilities reachable from our code."
  else
    finding
    # `go run` turns govulncheck's exit code 3 (findings) into 1, so look at the output instead.
    printf '%s\n' "$out" | grep -q '^Vulnerability #' || add "govulncheck could not run (exit code $rc)."
    # Keep the first two example traces per vulnerability; the rest is repetition.
    code "$(printf '%s\n' "$out" | grep -vE '^      #([3-9]|[0-9]{2,}):|^go: downloading')"
  fi
fi

# ---- npm ------------------------------------------------------------------
while IFS= read -r lock; do
  [ -z "$lock" ] && continue
  dir=$(dirname "$lock")
  section "npm audit ($dir)"
  out=$(cd "$dir" && npm audit --audit-level=high 2>&1)
  rc=$?
  if [ $rc -eq 0 ]; then
    add "No findings at high severity or above."
  else
    finding
    code "$out"
  fi
done < <(find_files package-lock.json)

# ---- Dockerfiles ----------------------------------------------------------
latest_node() { # $1 = major; newest released version on that line
  curl -fsS https://nodejs.org/dist/index.json | python3 -c '
import json, sys
major = sys.argv[1]
for r in json.load(sys.stdin):
    if r["version"].startswith("v" + major + "."):
        print(r["version"]); break' "$1"
}

seen_node=" "
while IFS= read -r df; do
  [ -z "$df" ] && continue
  # Stage names (FROM x AS name) are not images; "$" means a build arg we cannot resolve.
  stages=$(awk 'toupper($1)=="FROM"{for(i=2;i<=NF;i++) if(toupper($i)=="AS") print $(i+1)}' "$df")
  images=$(awk 'toupper($1)=="FROM"{for(i=2;i<=NF;i++) if($i !~ /^--/){print $i; break}}' "$df")

  # Node base images: the tag floats, so a cached layer can lag behind a security release.
  for img in $(printf '%s\n' "$images" | grep -E '^node:[0-9]+' | sort -u); do
    case "$seen_node" in *" $img "*) continue ;; esac
    seen_node="$seen_node$img "
    section "Node image ($img, from $df)"
    major=${img#node:}; major=${major%%[-.]*}
    if ! docker pull -q "$img" >/dev/null 2>&1; then finding; add "Could not pull $img."; continue; fi
    have=$(docker run --rm "$img" node -v 2>/dev/null)
    want=$(latest_node "$major")
    if [ -z "$have" ] || [ -z "$want" ]; then
      finding; add "Could not determine the version (image: '${have:-?}', nodejs.org: '${want:-?}')."
    elif [ "$have" != "$want" ]; then
      finding
      add "The image has **$have**, but the newest $major.x is **$want**. A build now would ship an old version or reuse a cached base layer. Docker Hub can lag a few hours behind a release, so check again tomorrow if the release is brand new."
    else
      add "The image is on the latest version ($have)."
    fi
  done

  # Runtime stage = last FROM. Build stages never ship, so they are not scanned for OS packages.
  last=$(printf '%s\n' "$images" | tail -n 1)
  if [ -n "$last" ] && [ "$last" != "scratch" ] && [[ "$last" != *'$'* ]] && ! printf '%s\n' "$stages" | grep -qx "$last"; then
    section "Trivy ($last, from $df)"
    ignore=()
    [ -f .trivyignore ] && ignore=(-v "$PWD/.trivyignore:/.trivyignore:ro" -e TRIVY_IGNOREFILE=/.trivyignore)
    json=$(docker run --rm ${ignore[@]+"${ignore[@]}"} aquasec/trivy:latest image --quiet --format json --severity HIGH,CRITICAL --ignore-unfixed "$last" 2>/dev/null)
    rc=$?
    if [ $rc -ne 0 ] || ! printf '%s' "$json" | jq -e . >/dev/null 2>&1; then
      finding; add "Trivy could not scan $last."
    else
      out=$(printf '%s' "$json" | jq -r '.Results[]? | .Target as $t | (.Vulnerabilities // [])[] | "\(.Severity)  \(.VulnerabilityID)  \(.PkgName) \(.InstalledVersion) -> \(.FixedVersion)  [\($t)]"' | sort -u)
      if [ -z "$out" ]; then
        add "No HIGH/CRITICAL findings with a fix available."
      else
        finding
        code "$out"
      fi
    fi
  fi
done < <(find_files Dockerfile)

if [ "$FAIL" -eq 0 ]; then
  add ""
  add "All clean."
fi
exit "$FAIL"
