FROM golang:1.23.6-bookworm

RUN apt-get update && apt-get install -y \
  git \
  protobuf-compiler='3.21.12*' \
  clang-format='1:14.0*'

# We don't want any default values for these variables to make sure they're
# explicitly provided by parsing the go.mod file. Otherwise we might forget to
# update them here if we bump the versions.
ARG PROTOBUF_VERSION
ARG GRPC_GATEWAY_VERSION
ARG LND_VERSION

ENV PROTOC_GEN_GO_GRPC_VERSION="v1.1.0"
ENV FALAFEL_VERSION="v0.9.1"
ENV GOCACHE=/tmp/build/.cache
ENV GOMODCACHE=/tmp/build/.modcache
ENV LND_VERSION=${LND_VERSION}

RUN cd /tmp \
  && mkdir -p /tmp/build/.cache \
  && mkdir -p /tmp/build/.modcache \
  && go install google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOBUF_VERSION} \
  && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@${PROTOC_GEN_GO_GRPC_VERSION} \
  && go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@${GRPC_GATEWAY_VERSION} \
  && go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@${GRPC_GATEWAY_VERSION} \
  && go install github.com/lightninglabs/falafel@${FALAFEL_VERSION} \
  && go install golang.org/x/tools/cmd/goimports@v0.1.7 \
  && go mod download github.com/lightningnetwork/lnd@${LND_VERSION} \
  && chmod -R 777 /tmp/build/

WORKDIR /build

CMD ["/bin/bash", "/build/taprpc/gen_protos.sh"]
