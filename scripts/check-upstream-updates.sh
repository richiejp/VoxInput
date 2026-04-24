#!/usr/bin/env bash
# Checks whether the LocalVQE git submodule and the GGUF model pinned in
# CMakeLists.txt are out of date with their upstreams.
#
# Exit codes:
#   0  everything up to date
#   1  one or more updates available (details printed to stdout)
#   2  hard error (network, parsing, missing tools)
#
# Designed to be run from CI weekly and locally on demand. All findings
# are written to stdout in Markdown so the workflow can pipe the output
# straight into a GitHub issue.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

HF_REPO="LocalAI-io/LocalVQE"
HF_API="https://huggingface.co/api/models/${HF_REPO}/tree/main"
SUBMODULE_PATH="LocalVQE"
SUBMODULE_BRANCH="main"

for cmd in curl jq git; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "error: required command not found: $cmd" >&2
        exit 2
    fi
done

updates_found=0
report=""

append() {
    report+="$1"$'\n'
}

# Extract the integer N from "localvqe-vN-f32.gguf".
model_version() {
    [[ $1 =~ ^localvqe-v([0-9]+)-f32\.gguf$ ]] && echo "${BASH_REMATCH[1]}"
}

submodule_url=$(git config --file .gitmodules "submodule.${SUBMODULE_PATH}.url")
pinned_sha=$(git ls-files -s "$SUBMODULE_PATH" | awk '$1 == "160000" {print $2}')
if [[ -z "$pinned_sha" ]]; then
    echo "error: could not resolve pinned submodule SHA for $SUBMODULE_PATH" >&2
    exit 2
fi

upstream_sha=$(git ls-remote "$submodule_url" "refs/heads/${SUBMODULE_BRANCH}" | awk '{print $1}')
if [[ -z "$upstream_sha" ]]; then
    echo "error: git ls-remote returned no SHA for ${submodule_url} ${SUBMODULE_BRANCH}" >&2
    exit 2
fi

append "## LocalVQE submodule"
append ""
append "- URL: ${submodule_url}"
append "- Pinned: \`${pinned_sha}\`"
append "- Upstream \`${SUBMODULE_BRANCH}\`: \`${upstream_sha}\`"

if [[ "$pinned_sha" != "$upstream_sha" ]]; then
    updates_found=1
    append ""
    append "**Update available.** Bump the submodule:"
    append ""
    append '```sh'
    append "git -C ${SUBMODULE_PATH} fetch origin ${SUBMODULE_BRANCH}"
    append "git -C ${SUBMODULE_PATH} checkout ${upstream_sha}"
    append "git add ${SUBMODULE_PATH}"
    append '```'
fi

append ""

api_json=$(curl -fsSL "$HF_API")

model_entries=()
while IFS= read -r line; do
    [[ -n "$line" ]] && model_entries+=("$line")
done < <(
    printf '%s' "$api_json" \
        | jq -r '
            .[]
            | select(.path | test("^localvqe-v[0-9]+-f32\\.gguf$"))
            | "\(.path)\t\(.lfs.oid // "NO_LFS")\t\(.size)"
          ' \
        | sort
)

if [[ ${#model_entries[@]} -eq 0 ]]; then
    echo "error: no localvqe-v*-f32.gguf files found at ${HF_REPO}" >&2
    exit 2
fi

pinned_url=$(sed -nE 's/^set\(LOCALVQE_MODEL_URL "([^"]+)".*/\1/p' CMakeLists.txt)
pinned_hash=$(sed -nE 's/.*EXPECTED_HASH SHA256=([0-9a-f]+).*/\1/p' CMakeLists.txt)

pinned_filename=$(basename "$pinned_url")

append "## LocalVQE GGUF model"
append ""
append "- Pinned URL: ${pinned_url}"
append "- Pinned SHA256: \`${pinned_hash}\`"
append ""
append "### Files on Hugging Face (\`${HF_REPO}\`)"
append ""
append "| File | SHA256 | Size |"
append "| --- | --- | --- |"

pinned_version=$(model_version "$pinned_filename")

for entry in "${model_entries[@]}"; do
    IFS=$'\t' read -r path oid size <<<"$entry"
    marker=""
    if [[ "$path" == "$pinned_filename" ]]; then
        if [[ "$oid" == "$pinned_hash" ]]; then
            marker=" (pinned, up to date)"
        else
            marker=" (pinned, **hash differs**)"
            updates_found=1
        fi
    else
        version=$(model_version "$path")
        if [[ -n "$version" && -n "$pinned_version" && "$version" -gt "$pinned_version" ]]; then
            marker=" (**newer version available**)"
            updates_found=1
        fi
    fi
    append "| \`${path}\`${marker} | \`${oid}\` | ${size} |"
done

append ""

if [[ $updates_found -eq 0 ]]; then
    append "_No updates available — everything is current._"
fi

printf '%s' "$report"

exit $updates_found
