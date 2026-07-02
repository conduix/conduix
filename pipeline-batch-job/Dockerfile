FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

# Copy all modules (shared, pipeline-core, pipeline-batch-job)
COPY shared/ ./shared/
COPY pipeline-core/ ./pipeline-core/
COPY pipeline-batch-job/ ./pipeline-batch-job/

# Build pipeline-batch-job
WORKDIR /app/pipeline-batch-job
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/runner ./cmd/runner

# Runtime
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/runner .

EXPOSE 8082

ENTRYPOINT ["./runner"]
