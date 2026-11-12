#!/usr/bin/env bash
# Builds the documentation once per release tag plus `main`, into one site:
#
#   /            the latest release (so the address people know shows what `@latest` installs)
#   /vX.Y.Z/     every release tag
#   /next/       main
#   /versions.json   what the version picker reads
#
# Usage: docs/build-versions.sh <repo-dir> <output-dir>
# Needs Ruby with the gems in docs/Gemfile (bundle install), and git history with tags.
set -euo pipefail

repo=$(cd "${1:?repo dir}" && pwd)
out=$(mkdir -p "${2:?output dir}" && cd "$2" && pwd)
site_base=${DOCS_BASEURL:-/migro}
site_url=${DOCS_URL:-https://louisriverstone.github.io}
work=$(mktemp -d)
trap 'git -C "$repo" worktree prune; rm -rf "$work"' EXIT

git -C "$repo" config --global --add safe.directory "$repo" 2>/dev/null || true

# Versions newest first; a pre-release (v1.0.0-rc.1) sorts before its final release.
mapfile -t tags < <(git -C "$repo" -c versionsort.suffix=- tag -l 'v*' --sort=-v:refname)
latest=""
for t in "${tags[@]}"; do
  [[ $t != *-* ]] && { latest=$t; break; }
done

built=0
# build <ref> <name> <baseurl> <dest>
build() {
  built=$((built + 1))
  local ref=$1 name=$2 base=$3 dest=$4 wt="$work/tree$built"
  git -C "$repo" worktree add --detach -q "$wt" "$ref"
  # Old tags predate the picker: give them the picker's template from the checked-out main.
  mkdir -p "$wt/docs/_includes"
  cp "$repo/docs/_includes/header_custom.html" "$wt/docs/_includes/header_custom.html"
  cat > "$work/$name$built.yml" <<YML
url: $site_url
baseurl: $base
docs_version: $name
docs_versions_url: $site_base/versions.json
YML
  echo "::group::docs $name -> $base"
  if (cd "$wt/docs" && BUNDLE_GEMFILE="$repo/docs/Gemfile" bundle exec jekyll build --config _config.yml,"$work/$name$built.yml" -d "$dest"); then
    echo "::endgroup::"
    return 0
  fi
  echo "::endgroup::"
  echo "::warning::docs for $name failed to build and were skipped"
  return 1
}

versions=()
add_version() { versions+=("{\"name\":\"$1\",\"label\":\"$2\",\"path\":\"$3\"}"); }

build HEAD next "$site_base/next" "$out/next" && add_version next "next (main)" "$site_base/next"

for t in "${tags[@]}"; do
  if build "$t" "$t" "$site_base/$t" "$out/$t"; then
    label=$t
    [[ $t == "$latest" ]] && label="$t (latest)"
    path="$site_base/$t"
    [[ $t == "$latest" ]] && path="$site_base"
    add_version "$t" "$label" "$path"
  fi
done

if [[ -n $latest ]]; then
  # The root is a second copy of the latest release, built for the root address.
  build "$latest" "$latest" "$site_base" "$out.root" || true
  cp -R "$out.root/." "$out/" && rm -rf "$out.root"
else
  # No release yet: keep the site alive by serving main at the root.
  build HEAD next "$site_base" "$out.root"
  cp -R "$out.root/." "$out/" && rm -rf "$out.root"
fi

# latest first, then older releases, main last.
python3 - "$out/versions.json" "$latest" "${versions[@]}" <<'PY'
import json, sys
dest, latest, *items = sys.argv[1:]
vs = [json.loads(i) for i in items]
nxt = [v for v in vs if v["name"] == "next"]
rel = [v for v in vs if v["name"] != "next"]
json.dump({"latest": latest, "versions": rel + nxt}, open(dest, "w"), indent=2)
PY
echo "built: ${versions[*]}"
