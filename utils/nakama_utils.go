package utils

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"game-server/systems/shared_constants"
	"io"

	"github.com/heroiclabs/nakama-common/runtime"
)

// LoadBaseJsonData
// first try load by storage if failed load by path
func LoadBaseJsonData(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, collectionName, keyName, baseDataPath string) (string, error) {

	//return fmt.Sprintf(`{"succeded": %t, "err" : %s}`, false, err.Error()), err

	if readStr, err := ReadServerStorageObjectByKey(ctx, nk, collectionName, keyName); err != nil {

		readFile, err := (*nk).ReadFile(baseDataPath)
		if err != nil {
			if err := readFile.Close(); err != nil {
				return fmt.Sprintf(`{"succeded": %t, "err" : %s}`, false, err.Error()), err
			}
			(*logger).Error("Error ReadFile form objectKey : ", keyName, " , baseDataPath : ", baseDataPath, err)
			return fmt.Sprintf(`{"succeded": %t, "err" : %s}`, false, err.Error()), err
		}

		// Read the file content
		byteValue, err := io.ReadAll(readFile)
		if err != nil {

			(*logger).Error("Error reading data form readFile so panic : %v", err)

			if err := readFile.Close(); err != nil {
				return fmt.Sprintf(`{"succeded": %t, "err" : %s}`, false, err.Error()), err
			}

			return fmt.Sprintf(`{"succeded": %t, "err" : %s}`, false, err.Error()), err
		}

		if err := readFile.Close(); err != nil {
			return fmt.Sprintf(`{"succeded": %t, "err" : %s}`, false, err.Error()), err
		}

		jsonData := string(byteValue)
		if err := WriteServerStorageObjectByKey(ctx, nk, collectionName, keyName, &jsonData); err != nil {
			(*logger).Error("Error WriteInventorySystem  objectKey : ", keyName, err)
			return fmt.Sprintf(`{"succeded": %t, "err" : %s}`, false, err.Error()), err
		}

		// there was no data in storage object so we read from server and wrote a storage object
		return jsonData, nil
	} else {
		return readStr, nil
	}
}

func WriteServerStorageObjectByKey(ctx *context.Context, nk *runtime.NakamaModule, collectionName, keyName string, payload *string) error {
	objectID := []*runtime.StorageWrite{&runtime.StorageWrite{
		Collection: collectionName,
		Key:        keyName,
		UserID:     shared_constants.ServerSystemUserId,
		Value:      *payload, // Value must be a valid encoded JSON object.
	}}

	_, err := (*nk).StorageWrite(*ctx, objectID)
	return err
}

func ReadServerStorageObjectByKey(ctx *context.Context, nk *runtime.NakamaModule, collectionName, keyName string) (string, error) {

	objectId := []*runtime.StorageRead{&runtime.StorageRead{
		Collection: collectionName,
		Key:        keyName,
		UserID:     shared_constants.ServerSystemUserId,
	}}

	records, err := (*nk).StorageRead(*ctx, objectId)
	if err != nil {
		return "", err
	} else {

		if records != nil {
			if len(records) > 0 {
				return records[0].Value, nil
			}
		}
		return "", errors.New("records is nil or empty")

	}
}

func convertToFloat64(value any) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	default:
		return 0
	}
}

func ReadUserStorageObject(ctx context.Context, nk runtime.NakamaModule, userID, collection, key string) (string, error) {
	records, err := nk.StorageRead(ctx, []*runtime.StorageRead{
		{Collection: collection, Key: key, UserID: userID},
	})
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", nil
	}
	return records[0].Value, nil
}

// ComputePercentile calculates percentile using the formula: (L + 0.5 × E) / N × 100
// L = players with score below, E = players with equal score, N = total eligible players
func ComputePercentile(ctx context.Context, db *sql.DB, leaderboardID string, score int64) float64 {
	var L, E, N int64

	db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM leaderboard_record WHERE leaderboard_id = $1 AND score < $2",
		leaderboardID, score).Scan(&L)

	db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM leaderboard_record WHERE leaderboard_id = $1 AND score = $2",
		leaderboardID, score).Scan(&E)

	db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM leaderboard_record WHERE leaderboard_id = $1",
		leaderboardID).Scan(&N)

	if N == 0 {
		return 0
	}
	return (float64(L) + 0.5*float64(E)) / float64(N)
}

func WriteUserStorageObject(ctx context.Context, nk runtime.NakamaModule, userID, collection, key, value string) error {
	_, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{
		{
			Collection:      collection,
			Key:             key,
			UserID:          userID,
			Value:           value,
			PermissionRead:  1,
			PermissionWrite: 0,
		},
	})
	return err
}

// UpdateMetaData
// like on int type will work like wallet change set
// else set(create or update) the key:value
func UpdateMetaData(ctx *context.Context, nk *runtime.NakamaModule, logger *runtime.Logger, userID string, updateMetadata map[string]any, forceToSet bool) error {

	/*	// ---------------------------- userID ----------------------------
		userID, ok := (*ctx).Value(runtime.RUNTIME_CTX_USER_ID).(string)
		if !ok {
			return errors.New("context did not contain a valid user ID.")
		}
	*/
	// ---------------------------- Read account ----------------------------

	account, err := (*nk).AccountGetId(*ctx, userID)
	if err != nil {
		return err
	}

	// ---------------------------- Read MetaData ----------------------------
	metadataString := account.User.Metadata

	var metadata map[string]interface{}

	if err := DeserializeObjectFromStringByRefsToMap(&metadataString, &metadata); err != nil {
		return err
	}

	// ---------------------------- Update MetaData Map ----------------------------

	for key, value := range updateMetadata {
		existingValue, exists := metadata[key]

		if forceToSet {
			// Note : will add or set
			metadata[key] = value
		} else {

			switch v := value.(type) {
			case float64, int:
				{
					changeValue := convertToFloat64(v)
					var newValue float64
					if exists {
						if previousValue, ok := existingValue.(float64); ok {
							newValue = previousValue + changeValue

							if newValue < 0 {
								metadata[key] = 0
							} else {
								metadata[key] = newValue
							}
						} else {
							metadata[key] = 0
						}
					} else {
						newValue = changeValue
						if newValue < 0 {
							metadata[key] = 0
						} else {
							metadata[key] = newValue
						}
					}

					break
				}
			default:
				metadata[key] = value
			}

		}

	}

	// ---------------------------- Update account MetaData ----------------------------

	if err := (*nk).AccountUpdateId(*ctx, userID, "", metadata, "", "", "", "", ""); err != nil {
		return err
	}

	return nil
}
