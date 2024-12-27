# Copyright 2019 The Volcano Authors.
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

BIN_DIR=_output/bin
RELEASE_DIR=_output/release
REPO_PATH=volcano.sh/descheduler
IMAGE_PREFIX=volcanosh
CC ?= "gcc"
SUPPORT_PLUGINS ?= "no"
CRD_VERSION ?= v1
BUILDX_OUTPUT_TYPE ?= "docker"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

OS=$(shell uname -s | tr '[:upper:]' '[:lower:]')

# Get OS architecture
OSARCH=$(shell uname -m)
ifeq ($(OSARCH),x86_64)
GOARCH?=amd64
else ifeq ($(OSARCH),x64)
GOARCH?=amd64
else ifeq ($(OSARCH),aarch64)
GOARCH?=arm64
else ifeq ($(OSARCH),aarch64_be)
GOARCH?=arm64
else ifeq ($(OSARCH),armv8b)
GOARCH?=arm64
else ifeq ($(OSARCH),armv8l)
GOARCH?=arm64
else ifeq ($(OSARCH),i386)
GOARCH?=x86
else ifeq ($(OSARCH),i686)
GOARCH?=x86
else ifeq ($(OSARCH),arm)
GOARCH?=arm
else
GOARCH?=$(OSARCH)
endif

# Run `make image DOCKER_PLATFORMS="linux/amd64,linux/arm64" BUILDX_OUTPUT_TYPE=registry IMAGE_PREFIX=[yourregistry]` to push multi-platform
DOCKER_PLATFORMS ?= "linux/${GOARCH}"

GOOS ?= linux

include Makefile.def

.EXPORT_ALL_VARIABLES:

all: vc-descheduler

init:
	mkdir -p ${BIN_DIR}
	mkdir -p ${RELEASE_DIR}

vc-descheduler: init
	CC=${CC} CGO_ENABLED=0 go build -ldflags ${LD_FLAGS} -o ${BIN_DIR}/vc-descheduler ./cmd/descheduler

image_bins: vc-descheduler

image:
	for name in descheduler; do\
		docker buildx build -t "${IMAGE_PREFIX}/vc-$$name:$(TAG)" . -f ./installer/dockerfile/$$name/Dockerfile --output=type=${BUILDX_OUTPUT_TYPE} --platform ${DOCKER_PLATFORMS} --build-arg APK_MIRROR=${APK_MIRROR} --build-arg OPEN_EULER_IMAGE_TAG=${OPEN_EULER_IMAGE_TAG}; \
	done

unit-test:
	go clean -testcache
	if [ ${OS} = 'darwin' ];then\
		go list ./... | grep -v "/e2e" | GOOS=${OS} xargs go test;\
	else\
		go test -p 8 -race $$(find pkg cmd -type f -name '*_test.go' | sed -r 's|/[^/]+$$||' | sort | uniq | sed "s|^|volcano.sh/descheduler/|");\
	fi;

clean:
	rm -rf _output/
	rm -f *.log

verify:
	hack/verify-gofmt.sh

mod-download-go:
	@-GOFLAGS="-mod=readonly" find -name go.mod -execdir go mod download \;
# go mod tidy is needed with Golang 1.16+ as go mod download affects go.sum
# https://github.com/golang/go/issues/43994
# exclude docs folder
	@find . -path ./docs -prune -o -name go.mod -execdir go mod tidy \;
