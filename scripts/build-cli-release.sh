#!/bin/sh
# Builds ivaldictl for every platform it ships on and writes checksums.
#
#   scripts/build-cli-release.sh <version> <outdir>
#
# The archives are attached to the component's GitHub Release; the version is
# what scripts/version.sh printed for the tagged commit.
set -eu

usage() {
	echo "usage: $0 <version> <outdir>" >&2
	exit 2
}

[ $# -eq 2 ] || usage
version=$1
out=$2

root=$(cd "$(dirname "$0")/.." && pwd)
commit=$(git -C "$root" rev-parse --short HEAD)
built=$(git -C "$root" log -1 --format=%cI)

# The CLI's version lives in its own package, so the linker is pointed there.
ldflags="-s -w \
	-X github.com/ivaldibuild/ivaldi-cli/internal/cli.VERSION=${version} \
	-X github.com/ivaldibuild/ivaldi-cli/internal/cli.COMMIT=${commit} \
	-X github.com/ivaldibuild/ivaldi-cli/internal/cli.BUILDTIME=${built}"

mkdir -p "$out"

for os in linux darwin windows; do
	for arch in amd64 arm64; do
		name="ivaldictl_${version}_${os}_${arch}"
		dir="$out/$name"
		binary="ivaldictl"
		[ "$os" = windows ] && binary="ivaldictl.exe"

		mkdir -p "$dir"
		# CGO off and -trimpath keep the binary portable and identical for a
		# rebuild of the same commit.
		CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -C "$root" -trimpath -ldflags "$ldflags" \
			-o "$dir/$binary" ./apps/cli/cmd

		if [ "$os" = windows ]; then
			(cd "$dir" && zip -q "$out/$name.zip" "$binary")
		else
			tar -czf "$out/$name.tar.gz" -C "$dir" "$binary"
		fi
		rm -rf "$dir"
	done
done

(cd "$out" && sha256sum ./*.tar.gz ./*.zip >checksums.txt)

echo "Wrote ivaldictl ${version} archives and checksums to $out"
