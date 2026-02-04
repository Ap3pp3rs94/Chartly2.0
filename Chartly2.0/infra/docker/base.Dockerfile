# syntax=docker/dockerfile:1.7
# Chartly 2.0  Base runtime image (non-root, distroless)
#
# Intended as a minimal, provider-neutral runtime base for Go services.
# This image contains no shell. Extend in service Dockerfiles.

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /
USER nonroot:nonroot
