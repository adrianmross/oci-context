#!/usr/bin/env bash
set -euo pipefail

repo="adrianmross/oci-context"
prefix="${PREFIX:-/usr/local}"
bin_dir="${prefix}/bin"
tool="${TOOL:-oci-context}" # oci-context or oci-contextd
version="${VERSION:-latest}" # latest, prerelease, or explicit tag (e.g. v0.1.0)

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required dependency: $1" >&2
    exit 1
  fi
}

for cmd in curl jq tar install grep awk uname mktemp; do
  require_cmd "$cmd"
done

case "${tool}" in
  oci-context|oci-contextd) ;;
  *)
    echo "Unsupported TOOL '${tool}'. Use 'oci-context' or 'oci-contextd'." >&2
    exit 1
    ;;
esac

if [[ "${version}" == "latest" ]]; then
  version=$(curl -fsSL --retry 3 --retry-all-errors "https://api.github.com/repos/${repo}/releases/latest" | jq -r '.tag_name')
elif [[ "${version}" == "pre" || "${version}" == "prerelease" ]]; then
  version=$(curl -fsSL --retry 3 --retry-all-errors "https://api.github.com/repos/${repo}/releases" | jq -r '[.[] | select(.prerelease)] | sort_by(.published_at) | reverse | .[0].tag_name')
fi

if [[ -z "${version}" || "${version}" == "null" ]]; then
  echo "Unable to determine release version" >&2
  exit 1
fi

uname_s=$(uname -s | tr '[:upper:]' '[:lower:]')
uname_m=$(uname -m)
case "${uname_m}" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *)
    echo "unsupported arch: ${uname_m}" >&2
    exit 1
    ;;
esac

version_no_v="${version#v}"
asset="${tool}_${version_no_v}_${uname_s}_${arch}.tar.gz"
checksums_asset="checksums.txt"
tmp_dir=$(mktemp -d)
trap 'rm -rf "${tmp_dir}"' EXIT

curl -fsSL --retry 3 --retry-all-errors -o "${tmp_dir}/${asset}" "https://github.com/${repo}/releases/download/${version}/${asset}"
curl -fsSL --retry 3 --retry-all-errors -o "${tmp_dir}/${checksums_asset}" "https://github.com/${repo}/releases/download/${version}/${checksums_asset}"

expected_checksum=$(grep "  ${asset}$" "${tmp_dir}/${checksums_asset}" | awk '{print $1}')
if [[ -z "${expected_checksum}" ]]; then
  echo "Checksum not found for ${asset}" >&2
  exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  actual_checksum=$(shasum -a 256 "${tmp_dir}/${asset}" | awk '{print $1}')
elif command -v sha256sum >/dev/null 2>&1; then
  actual_checksum=$(sha256sum "${tmp_dir}/${asset}" | awk '{print $1}')
else
  echo "No suitable SHA-256 checksum tool found (tried 'shasum' and 'sha256sum')." >&2
  echo "Please install one of these tools and re-run the installer." >&2
  exit 1
fi
if [[ "${expected_checksum}" != "${actual_checksum}" ]]; then
  echo "Checksum mismatch for ${asset}" >&2
  echo "Expected: ${expected_checksum}" >&2
  echo "Actual:   ${actual_checksum}" >&2
  exit 1
fi

tar -xzf "${tmp_dir}/${asset}" -C "${tmp_dir}"

install -d "${bin_dir}"
install "${tmp_dir}/${tool}" "${bin_dir}/${tool}"

echo "Installed ${tool} to ${bin_dir}/${tool}"
