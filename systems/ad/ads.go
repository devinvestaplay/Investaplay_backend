package ads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"game-server/systems/currency"
	"game-server/systems/shared_constants"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	rpcIdResetupAdConfig     = "ad_resetup"
	rpcIdGetAdConfig         = "ad_get"
	rpcIdGetRewardedAdReward = "ad_get_rewarded_ad_reward"
	rpcIdSpinWheelReward     = "spin_wheel_reward_claim"

	AdJSONFilePath = "../configs/ad.json"
	AdKey          = "ad"
)

var adsConfig AdsConfig
var adsConfigJson string

func InitAdsSystem(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	if data, err := utils.LoadBaseJsonData(ctx, logger, nk, shared_constants.ContainerCollectionName, AdKey, AdJSONFilePath); err != nil {
		return err
	} else {
		adsConfigJson = data
		err := processAdConfigJSON(adsConfigJson)
		if err != nil {
			return err
		}

		(*logger).Info("adsConfigJson : ", adsConfigJson)
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdResetupAdConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

		adsConfigJson = payload
		err := processAdConfigJSON(adsConfigJson)
		if err != nil {
			return "", err
		}

		if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, shared_constants.ContainerCollectionName, AdKey, &payload); err == nil {
			return fmt.Sprintf(`{"succeeded": %t}`, true), nil
		} else {
			return fmt.Sprintf(`{"succeeded": %t, "err": %s}`, false, err.Error()), err

		}

	}); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdGetAdConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		return adsConfigJson, nil
	}); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdGetRewardedAdReward, getRewardedAdReward); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdSpinWheelReward, claimSpinWheelReward); err != nil {
		return err
	}

	return nil

}

func getRewardedAdReward(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	logger.Info("getRewardedAdReward userID: %s", userID)

	// ----------------------------- Payload -----------------------------
	// TODO add reward id name

	// ----------------------------- Wallet -----------------------------

	// Build wallet changeset + calculate score
	changeset := make(map[string]int64)
	for _, vc := range adsConfig.Rewarded.Rewards {
		changeset["coins"] += int64(vc.Amount)
	}
	metadata := map[string]interface{}{
		"ad_unit_id": adsConfig.Rewarded.AdUnitID.Android,
	}

	_, _, err := nk.WalletUpdate(ctx, userID, changeset, metadata, true)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotModified, err.Error()), err
	}

	return utils.CreateStatus(true), nil
}

type SpinWheelRewardClaimRequest struct {
	Coins int `json:"coins"`
}

func claimSpinWheelReward(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	var request SpinWheelRewardClaimRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	if !isAllowedSpinWheelReward(request.Coins) {
		return utils.CreateStatus(false, http.StatusBadRequest, "invalid spin wheel reward"), nil
	}

	if request.Coins <= 0 {
		return utils.CreateStatus(true, http.StatusOK, "no reward"), nil
	}

	changeset := map[string]int64{
		"coins": int64(request.Coins),
	}
	metadata := map[string]interface{}{
		"source": "spin_wheel",
	}

	updatedWallet, _, err := nk.WalletUpdate(ctx, userID, changeset, metadata, true)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotModified, err.Error()), err
	}

	updatedWalletJson, err := json.Marshal(updatedWallet)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	logger.Info("claimSpinWheelReward userID=%s coins=%d", userID, request.Coins)
	return utils.CreateStatus(true, http.StatusOK, string(updatedWalletJson)), nil
}

func isAllowedSpinWheelReward(coins int) bool {
	switch coins {
	case 0, 50, 100, 150, 200, 300, 500:
		return true
	default:
		return false
	}
}

func processAdConfigJSON(jsonData string) error {
	var data AdsConfig
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return err
	} else {
		adsConfig = data
	}
	return nil
}

// AdType represents the category of an ad (as int enum).
type AdType int

const (
	AdTypeRewarded     AdType = iota // 0
	AdTypeInterstitial               // 1
	AdTypeBanner                     // 2
)

type AdUnitID struct {
	Android string `json:"android"`
	IOS     string `json:"ios"`
}

type AdItem struct {
	Enabled  bool     `json:"enabled"`
	Type     AdType   `json:"type"` // 0=Rewarded, 1=Interstitial, 2=Banner
	AdUnitID AdUnitID `json:"ad_unit_id"`
}

type BannerAd struct {
	AdItem
}

type InterstitialAd struct {
	AdItem
	PlayFrequency int `json:"play_frequency"` // player after x matches
}

type RewardedAd struct {
	AdItem
	Rewards []currency.VirtualCurrency `json:"rewards"`
}

type AppKeys struct {
	Android string `json:"android"`
	IOS     string `json:"ios"`
}

type AdsConfig struct {
	AppKeys      AppKeys        `json:"app_keys"`
	TestMode     bool           `json:"test_mode"`
	Rewarded     RewardedAd     `json:"rewarded"`
	Interstitial InterstitialAd `json:"interstitial"`
	Banner       BannerAd       `json:"banner"`
}
