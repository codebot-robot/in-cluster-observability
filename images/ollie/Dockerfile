# Copyright 2026 Google LLC
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

# Two-stage build: golang builder -> distroless static.
# CGO disabled per repo convention; binary is fully static so the
# final image needs no libc.

ARG VERSION=v0.3.0-dev

FROM golang:1.25 AS builder
ARG VERSION
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/ollie \
    ./cmd/ollie

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/ollie /ollie
USER 65532:65532
ENTRYPOINT ["/ollie"]
