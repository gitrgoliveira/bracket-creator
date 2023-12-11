# Build stage
ARG GO_VERSION=1.21

# Build stage
FROM golang:${GO_VERSION} AS builder


ARG GIT_COMMIT
ARG VERSION

ENV CGO_ENABLED=0

WORKDIR /bracket-creator
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN echo "nonroot:x:65534:65534:Non root:/:" > /etc_passwd
RUN make go/build

# Final stage
FROM scratch

LABEL maintainer="Ricardo Oliveira @gitrgoliveira"

# Copy only the necessary files
COPY --from=builder /bracket-creator .
COPY --from=builder /etc_passwd /etc/passwd

# Use numeric user ID
USER 65534

EXPOSE 8080
ENTRYPOINT [ "bracket-creator" ]
CMD [ "serve", "--bind=0.0.0.0", "--port=8080" ]