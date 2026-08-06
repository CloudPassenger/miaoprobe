# This Dockerfile is intended to be used by GoReleaser (see .goreleaser.yaml
# `dockers_v2` section): it expects prebuilt, statically linked `miaoprobe`
# binaries to already be present in the build context, laid out per
# platform (e.g. linux/amd64/miaoprobe, linux/arm64/miaoprobe).
FROM gcr.io/distroless/static-debian13:nonroot

ARG TARGETPLATFORM
COPY $TARGETPLATFORM/miaoprobe /usr/local/bin/miaoprobe

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/miaoprobe"]
CMD ["serve"]
