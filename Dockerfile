# syntax=docker/dockerfile:1
FROM golang AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download && go mod verify

COPY . .
RUN make && ./bin/apiserver -h

FROM ubuntu:latest
LABEL org.opencontainers.image.source="https://github.com/tcuthbert/apiserver"
ARG UID=1001
ARG GID=1002

RUN apt-get update \
  && apt-get install ca-certificates -y && update-ca-certificates

RUN groupadd -g "${GID}" apiserver \
  && useradd --home-dir /app --create-home -u "${UID}" -g "${GID}" apiserver

WORKDIR /app
COPY --from=builder /app/bin/apiserver .

EXPOSE 5000
USER apiserver
CMD ["/app/apiserver"]
