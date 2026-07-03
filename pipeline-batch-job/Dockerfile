FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS TARGETARCH

WORKDIR /app

RUN apk add --no-cache git

# 로컬 replace 대상 모듈 전부 복사 (go.mod가 ../plugin-sdk 등을 참조)
COPY shared/ ./shared/
COPY pipeline-core/ ./pipeline-core/
COPY plugin-sdk/ ./plugin-sdk/
COPY pipeline-batch-job/ ./pipeline-batch-job/

# Build pipeline-batch-job
WORKDIR /app/pipeline-batch-job
RUN go mod download
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /app/runner ./cmd/runner

# Runtime
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/runner .

EXPOSE 8082

ENTRYPOINT ["./runner"]
