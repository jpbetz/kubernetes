#!/usr/bin/env bash

# Copyright 2025 The Kubernetes Authors.
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

# Generates scheduler view types from k8s.io/api using subset-gen,
# then runs deepcopy-gen and register-gen on the output.

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE[0]}")/../../../..
source "${KUBE_ROOT}/hack/lib/init.sh"
cd "${KUBE_ROOT}"

VIEWS_DIR="${KUBE_ROOT}/pkg/scheduler/apis/views"
VIEWS_PKG="k8s.io/kubernetes/pkg/scheduler/apis/views"
BOILERPLATE="hack/boilerplate/boilerplate.generatego.txt"
CODEGEN="${KUBE_ROOT}/staging/src/k8s.io/code-generator"

# Step 1: Run subset-gen to produce types.go + doc.go.
GOPROXY=off go run k8s.io/code-generator/cmd/subset-gen \
    --output-dir "${VIEWS_DIR}" \
    --output-pkg "${VIEWS_PKG}" \
    --config "${VIEWS_DIR}/apps/v1/config.yaml" \
    --go-header-file "${BOILERPLATE}" \
    k8s.io/api/apps/v1

# Step 2: Generate deepcopy functions.
source "${CODEGEN}/kube_codegen.sh"

kube::codegen::gen_helpers \
    --boilerplate "${BOILERPLATE}" \
    "${VIEWS_DIR}"

# Step 3: Generate register functions.
kube::codegen::gen_register \
    --boilerplate "${BOILERPLATE}" \
    "${VIEWS_DIR}"

# Step 4: Generate client, listers, and informers.
kube::codegen::gen_client \
    --with-watch \
    --output-dir "${VIEWS_DIR}" \
    --output-pkg "${VIEWS_PKG}" \
    --boilerplate "${BOILERPLATE}" \
    "${VIEWS_DIR}"
