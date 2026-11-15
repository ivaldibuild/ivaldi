#!/bin/sh
# Prints the version of one component, a valid SemVer. Each component is
# released on its own schedule, so each one has its own tags under its own
# prefix and one git describe cannot name them all:
#
#   ivaldi/v0.4.0            → ivaldi              0.4.0
#   ivaldi-k8s-agent/v0.9.0  → ivaldi-k8s-agent   0.9.0
#   ivaldictl/v1.0.0         → ivaldictl          1.0.0
#
# Three commits after a component's tag it prints 0.4.0-3-gabc1234, and while a
# component has no tag yet, 0.0.0-gabc1234. The control plane's image and the
# Kubernetes agent's image and chart carry these strings; the web app ships
# inside the control plane's image, so it needs none of its own.
#
# A working tree with uncommitted changes or untracked files (the build would
# include them; ignored files do not count) adds -dirty, which the control
# plane treats as not a release build.
set -eu

usage() {
	echo "usage: $0 <component>" >&2
	echo "components: ivaldi ivaldi-k8s-agent ivaldictl" >&2
	exit 2
}

[ $# -eq 1 ] || usage
component=$1

case "$component" in
ivaldi | ivaldi-k8s-agent | ivaldictl) ;;
*) echo "$0: unknown component '$component'" >&2 && usage ;;
esac

dirty=""
if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
	dirty="-dirty"
fi

if described=$(git describe --tags --match "$component/v[0-9]*" 2>/dev/null); then
	echo "${described#"$component/v"}${dirty}"
elif sha=$(git rev-parse --short HEAD 2>/dev/null); then
	echo "0.0.0-g${sha}${dirty}"
else
	echo "0.0.0-dev"
fi
