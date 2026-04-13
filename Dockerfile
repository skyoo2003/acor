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

FROM alpine:3.15.11

RUN addgroup app \
  && adduser -D -h /acor -G app appuser \
  && mkdir -p /acor \
  && chown appuser:app /acor
VOLUME /acor
WORKDIR /acor
COPY --from=builder --chown=appuser:app /github.com/skyoo2003/acor/dist/acor /usr/bin/acor

USER appuser
ENTRYPOINT ["acor"]
