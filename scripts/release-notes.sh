#!/bin/sh
# Prints the release notes for one component: the commits since its previous
# tag that touched the paths the component ships.
#
#   scripts/release-notes.sh ivaldi [commit]
#
# A tag is written last, once a human has read the draft the release workflow
# opens from this output.
set -eu

usage() {
	echo "usage: $0 <component> [commit]" >&2
	echo "components: ivaldi ivaldi-k8s-agent ivaldictl" >&2
	exit 2
}

[ $# -ge 1 ] && [ $# -le 2 ] || usage
component=$1
rev=${2:-HEAD}

case "$component" in
ivaldi) paths="apps/control-plane apps/webui pkg api/openapi.json" ;;
ivaldi-k8s-agent) paths="apps/k8s-agent deploy/helm/ivaldi-agent pkg" ;;
ivaldictl) paths="apps/cli pkg api/openapi.json" ;;
*) echo "$0: unknown component '$component'" >&2 && usage ;;
esac

sha=$(git rev-parse --short "$rev")

if previous=$(git describe --tags --abbrev=0 --match "$component/v[0-9]*" "$rev" 2>/dev/null); then
	echo "Changes since $previous:"
	range="$previous..$rev"
else
	echo "First release of $component:"
	range=$rev
fi

# shellcheck disable=SC2086 # paths is a list of path specs on purpose.
git log --no-merges --pretty='- %s (%h)' "$range" -- $paths || true
echo
echo "Built from $sha. Verification: the CI run on that commit."
