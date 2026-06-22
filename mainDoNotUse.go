package main

/*
func main() {

	//mt := match.LudoMatchFinishData{
	//	ArenaName: "2P_ONLINE_ARENA",
	//	Ranking:   map[arena.Rank]string{},
	//}
	//
	//mt.Ranking[arena.RankWinner] = "mamd"
	//mt.Ranking[arena.RankSecond] = "mamd"
	//
	//jsonBytes, err := json.MarshalIndent(mt, "", "  ")
	//if err != nil {
	//	fmt.Println("Error serializing:", err)
	//	return
	//}
	//fmt.Println(string(jsonBytes))

	ad := ads.AdsConfig{
		AppKeys: ads.AppKeys{
			Android: "240683d7d",
			IOS:     "",
		},
		TestMode: true,
		Rewarded: ads.RewardedAd{
			AdItem: ads.AdItem{Enabled: true,
				Type: ads.AdTypeRewarded,
				AdUnitID: ads.AdUnitID{
					Android: "fifxta9xzk7rs1lu",
					IOS:     "",
				}},
			Rewards: []currency.VirtualCurrency{currency.VirtualCurrency{
				Type:   1,
				Amount: 59,
			}},
		},
		Interstitial: ads.InterstitialAd{
			AdItem: ads.AdItem{Enabled: true,
				Type: ads.AdTypeInterstitial,
				AdUnitID: ads.AdUnitID{
					Android: "rb7endmr6x206gr5",
					IOS:     "",
				}},
			PlayFrequency: 3,
		},
		Banner: ads.BannerAd{
			AdItem: ads.AdItem{Enabled: true,
				Type: ads.AdTypeBanner,
				AdUnitID: ads.AdUnitID{
					Android: "jzozrmbeqxanno13",
					IOS:     "",
				},
			},
		}}

	jsonBytes, err := json.MarshalIndent(ad, "", "  ")
	if err != nil {
		fmt.Println("Error serializing:", err)
		return
	}
	fmt.Println(string(jsonBytes))

	fmt.Println("Hello World")
}
*/

/*
// ----------------- Main function to populate arenas -----------------
func main() {
	feeAmount := 100
	var currencyType currency.CurrencyType
	currencyType = currency.LudoCoin

	arenas := map[string]arena.LudoArenaItemData{}

	// Helper to create reward map (fixed to match new struct)
	makeRewards := func(mode arena.ArenaMode) map[arena.Rank][]currency.VirtualCurrency {
		rewards := map[arena.Rank][]currency.VirtualCurrency{}

		if mode == arena.Mode2POnline || mode == arena.Mode2PWithFriends {
			total := feeAmount * 2
			rewards[arena.RankWinner] = []currency.VirtualCurrency{
				{Type: currencyType, Amount: int(float64(total) * 0.8)},
			}
			rewards[arena.RankSecond] = []currency.VirtualCurrency{
				{Type: currencyType, Amount: int(float64(total) * 0.2)},
			}
			rewards[arena.RankLooser] = []currency.VirtualCurrency{
				{Type: currencyType, Amount: int(float64(total) * 0.05)},
			}
		} else if mode == arena.Mode4POnline || mode == arena.Mode4PWithFriends {
			total := feeAmount * 4
			rewards[arena.RankWinner] = []currency.VirtualCurrency{
				{Type: currencyType, Amount: int(float64(total) * 0.75)},
			}
			rewards[arena.RankSecond] = []currency.VirtualCurrency{
				{Type: currencyType, Amount: int(float64(total) * 0.15)},
			}
			rewards[arena.RankThird] = []currency.VirtualCurrency{
				{Type: currencyType, Amount: int(float64(total) * 0.10)},
			}
			rewards[arena.RankLooser] = []currency.VirtualCurrency{
				{Type: currencyType, Amount: int(float64(total) * 0.05)},
			}
		}
		return rewards
	}

	// Populate arenas
	arenaModes := []arena.ArenaMode{arena.Mode2POnline, arena.Mode2PWithFriends, arena.Mode4POnline, arena.Mode4PWithFriends}
	for _, mode := range arenaModes {
		name := string(mode) + "_ARENA"
		arenas[name] = arena.LudoArenaItemData{
			Name: name,
			Mode: mode,
			FeeCurrencyData: currency.VirtualCurrency{
				Type:   currencyType,
				Amount: feeAmount,
			},
			Rewards:    makeRewards(mode),
			JsonConfig: json.RawMessage("{}"),
		}
	}

	// Create arena data struct
	ludoArenaDatas := arena.LudoArenaDatas{
		Arenas: arenas,
	}

	// Serialize to JSON and print
	jsonBytes, err := json.MarshalIndent(ludoArenaDatas, "", "  ")
	if err != nil {
		fmt.Println("Error serializing:", err)
		return
	}

	fmt.Println(string(jsonBytes))
}
*/
