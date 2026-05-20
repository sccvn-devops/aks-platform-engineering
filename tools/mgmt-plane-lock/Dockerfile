FROM golang:1.24.3 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mgmt-leader-lease ./cmd/mgmt-leader-lease
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mgmt-cli ./cmd/mgmt-cli

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/mgmt-leader-lease /app/mgmt-leader-lease
USER nonroot:nonroot
ENTRYPOINT ["/app/mgmt-leader-lease"]
