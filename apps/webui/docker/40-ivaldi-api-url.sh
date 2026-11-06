#!/bin/sh
# Writes IVALDI_API_URL, the control plane's address, into the web app's
# <meta name="ivaldi-api-url"> tag before nginx starts. Leave it unset when the
# web app and the control plane share one origin: the tag is then emptied and
# the web app calls the API on its own origin.
#
# The whole tag is replaced, so the script gives the same result on every start.
set -eu

index=/usr/share/nginx/html/index.html
url=${IVALDI_API_URL:-}
url=${url%/}

# An origin only, like the control plane's external_url. Restricting the
# characters also keeps the value safe to place in the HTML attribute and in sed.
if [ -n "$url" ] && ! printf '%s' "$url" | grep -Eq '^https?://[A-Za-z0-9.-]+(:[0-9]+)?$'; then
	echo "$0: IVALDI_API_URL must be an origin such as https://ivaldi.example.com, with no path: got \"$url\"" >&2
	exit 1
fi

if ! grep -q '<meta name="ivaldi-api-url"' "$index"; then
	echo "$0: $index has no <meta name=\"ivaldi-api-url\"> tag" >&2
	exit 1
fi

out=$(mktemp)
sed -E "s|<meta name=\"ivaldi-api-url\"[^>]*>|<meta name=\"ivaldi-api-url\" content=\"$url\">|" "$index" >"$out"
cat "$out" >"$index"
rm -f "$out"

echo "$0: ivaldi-api-url set to \"$url\""
