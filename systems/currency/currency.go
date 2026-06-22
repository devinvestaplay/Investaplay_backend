package currency

type CurrencyType int

const (
	Coin CurrencyType = 1
)

type VirtualCurrency struct {
	Type   CurrencyType `json:"type"`
	Amount int          `json:"amount"`
	Name   string       `json:"name,omitempty"` // for handling inventory
}
