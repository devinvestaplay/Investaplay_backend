package wallet

import (
	"game-server/utils"
)

type WalletData struct {
	Coins int `json:"coins"`
}

func DeserializeWalletData(data *string) (*WalletData, error) {

	var walletData WalletData

	if err := utils.DeserializeObjectFromStringByRefs(data, &walletData); err != nil {
		return nil, err
	}
	return &walletData, nil
}
