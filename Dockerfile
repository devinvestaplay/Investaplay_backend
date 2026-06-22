FROM heroiclabs/nakama-pluginbuilder:3.32.0 AS builder

ENV GO111MODULE on
ENV CGO_ENABLED 1
ENV GOPRIVATE "game-server"

WORKDIR /backend
COPY go.mod go.sum ./
RUN go mod tidy && go mod vendor
COPY . .
RUN go build --trimpath --mod=vendor --buildmode=plugin -o ./backend.so

FROM heroiclabs/nakama:3.32.0

COPY --from=builder /backend/backend.so /nakama/data/modules

COPY --from=builder /backend/base_config.yml /nakama/data/

COPY --from=builder /backend/configs/* /nakama/data/configs/
