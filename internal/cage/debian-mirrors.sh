#!/bin/sh
# Only stock and Cage-owned lists are managed. Custom lists and direct URIs
# remain under the user's control. The preference survives guest cloning.
sudo env APT_CONFIG="${CAGE_APT_CONFIG:-/dev/null}" CAGE_DEBIAN_MIRROR="${CAGE_DEBIAN_MIRROR:-}" sh -se <<'CAGE_DEBIAN_MIRRORS'
set -eu
preference=auto
if [ -f /etc/claude-cage/debian-mirror ]; then
  preference=$(cat /etc/claude-cage/debian-mirror)
fi
# A choice made on the host after measuring the sources through the proxy
# wins over the saved file and is kept for later maintenance runs.
if [ -n "${CAGE_DEBIAN_MIRROR:-}" ]; then
  case "$CAGE_DEBIAN_MIRROR" in
    auto|official|domestic) ;;
    *) echo 'Invalid CAGE_DEBIAN_MIRROR: use auto, official or domestic.' >&2; exit 1;;
  esac
  preference=$CAGE_DEBIAN_MIRROR
  mkdir -p "$(dirname /etc/claude-cage/debian-mirror)"
  printf '%s\n' "$preference" > /etc/claude-cage/debian-mirror
fi
case "$preference" in
  auto|official|domestic) ;;
  *) echo 'Invalid /etc/claude-cage/debian-mirror: use auto, official or domestic.' >&2; exit 1;;
esac
for repository in debian debian-security; do
  list="/etc/apt/mirrors/$repository.list"
  [ -f "$list" ] || continue
  content=$(cat "$list")
  official="https://deb.debian.org/$repository"
  old=$(printf '%s\tpriority:%s\n' "https://mirrors.nju.edu.cn/$repository" 1 "https://mirrors.ustc.edu.cn/$repository" 2 "$official" 3)
  case "$content" in
    "$official"|"$old"|'# Cage managed Debian mirrors'*) ;;
    *) continue;;
  esac
  stage=$(mktemp "$list.XXXXXX")
  probe=$(mktemp)
  trap 'rm -f "$stage" "$probe"' EXIT
  printf '# Cage managed Debian mirrors\n' > "$stage"
  suite=$(. /etc/os-release; printf '%s' "${VERSION_CODENAME:-trixie}")
  [ "$repository" != debian-security ] || suite="$suite-security"
  priority=0
  # Prefer the official CDN when it works through the guest's configured
  # network. Probe domestic mirrors only after the official source fails,
  # unless the host measured them faster and chose "domestic".
  if [ "$preference" = domestic ] || { [ "$preference" = auto ] && ! timeout 8 /usr/lib/apt/apt-helper -o Acquire::Retries=0 \
      -o Acquire::http::Timeout=5 -o Acquire::https::Timeout=5 \
      download-file "$official/dists/$suite/InRelease" "$probe" >/dev/null 2>&1; }; then
    for mirror in "https://mirrors.nju.edu.cn/$repository" "https://mirrors.ustc.edu.cn/$repository"; do
      rm -f "$probe"
      if timeout 8 /usr/lib/apt/apt-helper -o Acquire::Retries=0 \
          -o Acquire::http::Timeout=5 -o Acquire::https::Timeout=5 \
          download-file "$mirror/dists/$suite/InRelease" "$probe" >/dev/null 2>&1; then
        priority=$((priority + 1))
        printf '%s\tpriority:%s\n' "$mirror" "$priority" >> "$stage"
      else
        printf 'Debian mirror unavailable; using the next source: %s\n' "$mirror"
      fi
    done
  fi
  priority=$((priority + 1))
  printf '%s\tpriority:%s\n' "$official" "$priority" >> "$stage"
  chmod 0644 "$stage"
  mv -f "$stage" "$list"
  rm -f "$probe"
  trap - EXIT
done
CAGE_DEBIAN_MIRRORS
