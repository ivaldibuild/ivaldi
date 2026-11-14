#!/bin/sh
# Prints the build's version, a valid SemVer, so the control plane, the agent
# binary, its image, and its Helm chart all carry the same string:
#
#   on a tag v0.4.0          0.4.0
#   three commits after it   0.4.0-3-gabc1234
#   with no tag yet          0.0.0-gabc1234
#
# A working tree with uncommitted changes adds -dirty, which the control plane
# treats as not a release build.
set -eu

dirty=""
if [ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ]; then
	dirty="-dirty"
fi

if described=$(git describe --tags --match 'v[0-9]*' 2>/dev/null); then
	echo "${described#v}${dirty}"
elif sha=$(git rev-parse --short HEAD 2>/dev/null); then
	echo "0.0.0-g${sha}${dirty}"
else
	echo "0.0.0-dev"
fi
