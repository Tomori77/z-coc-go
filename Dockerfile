FROM golang:1.26-alpine AS build
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/z-coc ./cmd/server

FROM alpine:3.20
COPY --from=build /out/z-coc /z-coc
EXPOSE 8080
ENTRYPOINT ["/z-coc"]
