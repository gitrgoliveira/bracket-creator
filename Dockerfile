ARG GO_VERSION=1.21

# Build stage
FROM golang:${GO_VERSION} AS builder

ARG GIT_COMMIT
ARG VERSION

ENV GO111MODULE=auto
ENV CGO_ENABLED=0

WORKDIR $GOPATH/src/github.com/gitrgoliveira/bracket-creator
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make go/build
RUN echo "nonroot:x:65534:65534:Non root:/:" > /etc_passwd


# Final stage
FROM scratch

LABEL maintainer="Ricardo Oliveira @gitrgoliveira"

COPY --from=builder /go/bin/bracket-creator /bin/bracket-creator
COPY --from=builder /etc_passwd /etc/passwd

USER nonroot

ENTRYPOINT [ "bracket-creator" ]
CMD [ "version" ]
