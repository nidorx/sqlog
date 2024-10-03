# syntax=docker/dockerfile:1

# We use a multi-stage build setup.
# (https://docs.docker.com/build/building/multi-stage/)

# Stage 1
FROM golang:1.23.2-alpine AS builder

# smoke test to verify if golang is available
RUN go version

RUN apk --no-cache add gcc g++ sqlite

ARG PROJECT_VERSION

WORKDIR /go/src/demo/
COPY . ./

RUN set -Eeux && go mod tidy && go mod download && go mod verify

RUN GOOS=linux CGO_ENABLED=1 go build -trimpath -ldflags="-w -s -X 'main.Version=${PROJECT_VERSION}'" -o app main.go

# Stage 2 
FROM alpine:3.20.3
RUN apk --no-cache add ca-certificates

WORKDIR /root/
COPY --from=builder /go/src/demo/app .

EXPOSE 8080
CMD ["/root/app"]