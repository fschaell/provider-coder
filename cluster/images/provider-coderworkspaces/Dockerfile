FROM alpine:latest as coder
RUN apk add --no-cache curl
RUN curl -s -L https://github.com/coder/coder-v1-cli/releases/download/v1.44.0/coder-cli-linux-amd64.tar.gz -o coder-cli.tar.gz
RUN tar -xzvf coder-cli.tar.gz -C /usr/local/bin

FROM gcr.io/distroless/static@sha256:a01d47d4036cae5a67a9619e3d06fa14a6811a2247b4da72b4233ece4efebd57
COPY --from=coder /usr/local/bin/coder /usr/local/bin/coder
ARG TARGETOS
ARG TARGETARCH

ADD bin/$TARGETOS\_$TARGETARCH/provider /usr/local/bin/crossplane-coderworkspaces-provider

USER 65532
ENTRYPOINT ["crossplane-coderworkspaces-provider"]