#!/usr/bin/env bash
# Cross-compiles release archives into dist/ and writes SHA256SUMS.
#
#   VERSION=v1.2.3 BUILD_NUMBER=42 scripts/build-release.sh
#
# Used by `make dist` and by the release workflow.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
BUILD_NUMBER="${BUILD_NUMBER:-0}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
PLATFORMS="${PLATFORMS:-linux/amd64 linux/arm64 linux/arm/7 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64}"
PKG=github.com/lobojoel2003/accountedfor/internal/version
LDFLAGS="-s -w -X ${PKG}.Version=${VERSION} -X ${PKG}.Commit=${COMMIT} -X ${PKG}.BuildNumber=${BUILD_NUMBER} -X ${PKG}.BuildDate=${BUILD_DATE}"

# Start from an empty dist/ directory in the repository root.
if [[ -d dist ]]; then
  find dist -mindepth 1 -delete
fi
mkdir -p dist

for platform in ${PLATFORMS}; do
  IFS=/ read -r goos goarch goarm <<<"${platform}"
  name="accountedfor_${VERSION#v}_${goos}_${goarch}${goarm:+v${goarm}}"
  stage="dist/${name}"
  mkdir -p "${stage}/examples"
  bin="accountedfor"
  [[ "${goos}" == "windows" ]] && bin="accountedfor.exe"
  echo "building ${name}"
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" GOARM="${goarm}" \
    go build -trimpath -ldflags "${LDFLAGS}" -o "${stage}/${bin}" ./cmd/accountedfor
  cp README.md LICENSE CHANGELOG.md .env.example docker-compose.yml "${stage}/"
  cp examples/*.xml "${stage}/examples/"
  if [[ "${goos}" == "windows" ]]; then
    (cd dist && zip -qr "${name}.zip" "${name}")
  else
    tar -C dist -czf "dist/${name}.tar.gz" "${name}"
  fi
  find "${stage}" -delete
done

(cd dist && sha256sum -- *.tar.gz *.zip > SHA256SUMS)
echo
cat dist/SHA256SUMS
