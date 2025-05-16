FROM golang:1.24 AS build

WORKDIR /app

COPY site-management-service/ .

RUN go mod download
RUN go build -o site-management-service .

FROM ghcr.io/netcracker/qubership/core-base:1.0.0 AS run

COPY --chown=10001:0 --chmod=555 --from=build app/site-management-service /app/site-management
COPY --chown=10001:0 --chmod=444 --from=build app/application.yaml /app/
COPY --chown=10001:0 --chmod=444 --from=build app/docs/swagger.json /app/
COPY --chown=10001:0 --chmod=444 --from=build app/docs/swagger.yaml /app/

WORKDIR /app

USER 10001:10001

CMD ["/app/site-management"]