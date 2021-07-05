FROM golang:1.16.4-alpine3.12 AS builder

ENV CGO_ENABLED=0
ENV GO111MODULE=on

RUN apk --no-cache --no-progress add --virtual \
  build-deps \
  build-base \
  git

WORKDIR /github.com/skyoo2003/acor
COPY . .
RUN make build

FROM alpine:3.13.5

VOLUME /acor
WORKDIR /acor
COPY --from=builder /github.com/skyoo2003/acor/dist/acor /usr/bin/acor

ENTRYPOINT ["acor"]
