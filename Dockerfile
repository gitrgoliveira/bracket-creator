# Build stage
ARG GO_VERSION=1.26.4

# Build stage
FROM golang:${GO_VERSION} AS builder


ARG GIT_COMMIT
ARG VERSION

ENV CGO_ENABLED=0

WORKDIR /bracket-creator

# Install Node.js for frontend build dependencies (npx esbuild)
RUN apt-get update && apt-get install -y curl && \
    curl -fsSL https://deb.nodesource.com/setup_26.x | bash - && \
    apt-get install -y nodejs && \
    rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN echo "nonroot:x:65534:65534:Non root:/:" > /etc_passwd
RUN make go/build

# Final stage
FROM scratch

LABEL maintainer="Ricardo Oliveira @gitrgoliveira"

# Copy only the compiled binary and password file
COPY --from=builder /bracket-creator/bin/bracket-creator /bin/bracket-creator
COPY --from=builder /etc_passwd /etc/passwd

# Use numeric user ID
USER 65534

EXPOSE 8080
ENTRYPOINT [ "/bin/bracket-creator" ]
CMD [ "serve", "--bind=0.0.0.0" ]
