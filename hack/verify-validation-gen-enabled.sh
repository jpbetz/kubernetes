#!/usr/bin/env bash

# Copyright 2026 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script verifies that every versioned API package under pkg/apis/
# opts in to declarative validation by declaring the +k8s:validation-gen
# tag in its doc.go. Without the tag, validation-gen produces no
# RegisterValidations call for the package, and any field-level validation
# markers added later are silently ignored.
#
# Usage: `hack/verify-validation-gen-enabled.sh`.

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
source "${KUBE_ROOT}/hack/lib/init.sh"

cd "${KUBE_ROOT}"

# DO NOT ADD NEW ENTRIES HERE.
# Packages whose types are not registered with the kube-apiserver scheme
# for REST serving and therefore do not need declarative validation
# plumbing. ABAC policy documents are configuration files loaded from
# disk, not REST API types.
EXEMPT_PACKAGES=(
  pkg/apis/abac/v0
  pkg/apis/abac/v1beta1
)

is_exempt() {
  local pkg="$1"
  local e
  for e in "${EXEMPT_PACKAGES[@]}"; do
    if [[ "${pkg}" == "${e}" ]]; then
      return 0
    fi
  done
  return 1
}

missing=()
for doc in pkg/apis/*/v[0-9]*/doc.go; do
  pkg=$(dirname "${doc}")
  if is_exempt "${pkg}"; then
    continue
  fi
  if ! grep -qE '^// \+k8s:validation-gen=' "${doc}"; then
    missing+=("${pkg}")
  fi
done

if (( ${#missing[@]} > 0 )); then
  {
    echo "Declarative validation is not enabled in the following API packages:"
    printf '  %s\n' "${missing[@]}"
    echo
    echo "Every versioned API package must opt in by adding the following lines to its doc.go:"
    echo
    echo "  // +k8s:validation-gen=TypeMeta"
    echo "  // +k8s:validation-gen-input=k8s.io/api/<group>/<version>"
    echo
    echo "See staging/src/k8s.io/code-generator/cmd/validation-gen for details."
  } >&2
  exit 1
fi
