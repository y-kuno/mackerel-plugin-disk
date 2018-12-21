#!/bin/sh

# Usage:
# $ script/release # Setting github.token in .gitconfig is required
# $ GITHUB_TOKEN=... script/release

set -e
latest_tag=$(git describe --abbrev=0 --tags)
goxz -d dist/$latest_tag -z -os darwin,linux -arch amd64,386
ghr -u y-kuno -r mackerel-plugin-disk $latest_tag dist/$latest_tag