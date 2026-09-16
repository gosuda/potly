FROM golang:1.27-alpine AS build
WORKDIR /src
ARG VERSION=dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /potly .

FROM gcr.io/distroless/static-debian12
COPY --from=build /potly /potly
WORKDIR /data
EXPOSE 8000
ENTRYPOINT ["/potly"]
