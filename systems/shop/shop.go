package shop

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
	rpcIdResetupShopConfig = "shop_config_resetup"
	rpcIdGetShopConfig     = "shop_config_get"
	rpcIdShopPurchaseItem  = "shop_purchase_item"

	BaseShopConfigJSONFilePath = "../configs/base-shop-config.json"
	ShopConfigKey              = "shop_config"
)

var shopConfigJson string
var shopProducts ShopProducts

func InitShopConfig(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	// Load Shop Config data
	if data, err := utils.LoadBaseJsonData(ctx, logger, nk, shared_constants.ContainerCollectionName, ShopConfigKey, BaseShopConfigJSONFilePath); err != nil {
		return err
	} else {
		shopConfigJson = data
		err := processShopProductsJSON(shopConfigJson)
		if err != nil {
			return err
		}
		(*logger).Info("shopConfigJson : ", shopConfigJson)
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdResetupShopConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

		// TODO add some user id as valid
		// TODO force to work only with system account
		shopConfigJson = payload
		err := processShopProductsJSON(shopConfigJson)
		if err != nil {
			return "", err
		}
		if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, shared_constants.ContainerCollectionName, ShopConfigKey, &payload); err == nil {
			return fmt.Sprintf(`{"succeeded": %t}`, true), nil
		} else {
			return fmt.Sprintf(`{"succeeded": %t, "err": %s}`, false, err.Error()), err

		}

	}); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdGetShopConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		return shopConfigJson, nil
	}); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdShopPurchaseItem, purchaseShopItem); err != nil {
		return err
	}

	return nil

}

func purchaseShopItem(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	logger.Info("purchaseShopItem userID: %s", userID)

	// ----------------------------- Payload -----------------------------

	var purchaseData ShopPurchaseData
	err := utils.DeserializeObjectFromStringByRefs(&payload, &purchaseData)
	if err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}
	product := shopProducts.Products[purchaseData.ProductID]

	// ----------------------------- Validate -----------------------------
	// TODO
	//nk.PurchaseValidateGoogle(ctx, userID, purchaseData.Receipt, true, struct {
	//	ClientEmail string
	//	PrivateKey  string
	//}{ClientEmail: , PrivateKey: } )
	// TODO

	// ----------------------------- Wallet -----------------------------

	// Build wallet changeset + calculate score
	changeset := make(map[string]int64)
	for _, vc := range product.Rewards {
		changeset["coins"] += int64(vc.Amount)
	}
	metadata := map[string]interface{}{
		"productID": product.ProductID,
		"market":    purchaseData.Market,
	}

	_, _, err = nk.WalletUpdate(ctx, userID, changeset, metadata, true)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotModified, err.Error()), err
	}

	return utils.CreateStatus(true), nil
}

// ---------------------------------------------

type ShopItemType string

const (
	Consumable    ShopItemType = "consumable"
	NonConsumable ShopItemType = "nonconsumable"
	Subscription  ShopItemType = "subscription"
	Unknown       ShopItemType = "unknown"
)

type ShopProducts struct {
	Products map[string]ShopProductData `json:"products"` // key : product id
}

type ShopProductData struct {
	Enabled    bool                       `json:"enabled"`
	Priority   int                        `json:"priority"`
	ProductID  string                     `json:"productID"`
	GoogleID   string                     `json:"googleID"`
	IosID      string                     `json:"iosID"`
	Type       ShopItemType               `json:"type"`
	Rewards    []currency.VirtualCurrency `json:"rewards"`
	JsonConfig json.RawMessage            `json:"json_config"`
}

func processShopProductsJSON(jsonData string) error {
	var data ShopProducts
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return err
	} else {
		shopProducts = data
	}
	return nil
}

// ---------------------------------------------

type Market int

const (
	GooglePlay Market = 0
	Apple      Market = 1
)

type ShopPurchaseData struct {
	Market    Market `json:"market"`
	ProductID string `json:"productID"`
	Receipt   string `json:"receipt"`
}
