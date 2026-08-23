FROM alpine:3.22

RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --chmod=0555 build/chaos /app/chaos

USER 65532:65532
ENTRYPOINT ["/app/chaos"]
