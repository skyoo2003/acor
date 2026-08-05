FROM golang:1.25-alpine3.21 AS builder

ENV CGO_ENABLED=0
ENV GO111MODULE=on

RUN apk --no-cache --no-progress add --virtual \
  build-deps \
  build-base \
  git

WORKDIR /github.com/skyoo2003/acor
COPY . .
RUN make build

FROM alpine:3.21

RUN addgroup -S -g 1000 app \
  && adduser -S -u 1000 -h /acor -G app appuser \
  && mkdir -p /acor \
  && chown appuser:app /acor
VOLUME /acor
WORKDIR /acor
COPY --from=builder --chown=appuser:app /github.com/skyoo2003/acor/dist/acor /usr/bin/acor
# Same path as Dockerfile.goreleaser, so the locally built and released images
# carry the third-party notices in the same place.
COPY --from=builder /github.com/skyoo2003/acor/LICENSE /github.com/skyoo2003/acor/NOTICE /usr/share/doc/acor/

USER appuser
ENTRYPOINT ["acor"]
