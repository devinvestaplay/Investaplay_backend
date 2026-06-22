package server_time

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	rpcIdTimeUnix = "time_unix"
)

func InitServerTime(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	if err := (*initializer).RegisterRpc(rpcIdTimeUnix, Time); err != nil {
		return err
	}
	return nil

}

func Time(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	return fmt.Sprintf(`{"time" : "%d"}`, time.Now().UTC().UnixMilli()), nil
}
