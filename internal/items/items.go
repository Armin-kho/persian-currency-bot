
package items

type Category string

const (
	CategoryCurrency Category = "currency"
	CategoryCoin     Category = "coin"
	CategoryGold     Category = "gold"
	CategoryCrypto   Category = "crypto"
)

const (
	UnitToman = "toman"
	UnitUSD   = "usd"
)

type Item struct {
	ID       string
	Category Category

	NameFa string
	Emoji  string

	// Bonbast mappings
	BonbastSellKey string
	BonbastBuyKey  string
	BonbastUnit    string

	// Navasan mappings
	// NavasanKey is the base key (we also try key+"_sell" and key+"_buy" automatically in the fetcher).
	NavasanKey     string
	NavasanSellKey string
	NavasanBuyKey  string
	NavasanUnit    string
	NavasanIsCrypto bool
}

var All = []Item{
	// -------- CURRENCIES --------
	{ID: "USD", Category: CategoryCurrency, NameFa: "دلار آمریکا", Emoji: "💵", BonbastSellKey: "usd1", BonbastBuyKey: "usd2", BonbastUnit: UnitToman, NavasanKey: "usd", NavasanSellKey: "usd_sell", NavasanBuyKey: "usd_buy", NavasanUnit: UnitToman},
	{ID: "EUR", Category: CategoryCurrency, NameFa: "یورو", Emoji: "💶", BonbastSellKey: "eur1", BonbastBuyKey: "eur2", BonbastUnit: UnitToman, NavasanKey: "eur", NavasanSellKey: "eur_sell", NavasanBuyKey: "eur_buy", NavasanUnit: UnitToman},
	{ID: "GBP", Category: CategoryCurrency, NameFa: "پوند انگلیس", Emoji: "💷", BonbastSellKey: "gbp1", BonbastBuyKey: "gbp2", BonbastUnit: UnitToman, NavasanKey: "gbp", NavasanSellKey: "gbp_sell", NavasanBuyKey: "gbp_buy", NavasanUnit: UnitToman},
	{ID: "CHF", Category: CategoryCurrency, NameFa: "فرانک سوئیس", Emoji: "💱", BonbastSellKey: "chf1", BonbastBuyKey: "chf2", BonbastUnit: UnitToman, NavasanKey: "chf", NavasanSellKey: "chf_sell", NavasanBuyKey: "chf_buy", NavasanUnit: UnitToman},
	{ID: "CAD", Category: CategoryCurrency, NameFa: "دلار کانادا", Emoji: "💱", BonbastSellKey: "cad1", BonbastBuyKey: "cad2", BonbastUnit: UnitToman, NavasanKey: "cad", NavasanSellKey: "cad_sell", NavasanBuyKey: "cad_buy", NavasanUnit: UnitToman},
	{ID: "AUD", Category: CategoryCurrency, NameFa: "دلار استرالیا", Emoji: "💱", BonbastSellKey: "aud1", BonbastBuyKey: "aud2", BonbastUnit: UnitToman, NavasanKey: "aud", NavasanSellKey: "aud_sell", NavasanBuyKey: "aud_buy", NavasanUnit: UnitToman},
	{ID: "SEK", Category: CategoryCurrency, NameFa: "کرون سوئد", Emoji: "💱", BonbastSellKey: "sek1", BonbastBuyKey: "sek2", BonbastUnit: UnitToman, NavasanKey: "sek", NavasanSellKey: "sek_sell", NavasanBuyKey: "sek_buy", NavasanUnit: UnitToman},
	{ID: "NOK", Category: CategoryCurrency, NameFa: "کرون نروژ", Emoji: "💱", BonbastSellKey: "nok1", BonbastBuyKey: "nok2", BonbastUnit: UnitToman, NavasanKey: "nok", NavasanSellKey: "nok_sell", NavasanBuyKey: "nok_buy", NavasanUnit: UnitToman},
	{ID: "RUB", Category: CategoryCurrency, NameFa: "روبل روسیه", Emoji: "💱", BonbastSellKey: "rub1", BonbastBuyKey: "rub2", BonbastUnit: UnitToman, NavasanKey: "rub", NavasanSellKey: "rub_sell", NavasanBuyKey: "rub_buy", NavasanUnit: UnitToman},
	{ID: "THB", Category: CategoryCurrency, NameFa: "بات تایلند", Emoji: "💱", BonbastSellKey: "thb1", BonbastBuyKey: "thb2", BonbastUnit: UnitToman, NavasanKey: "thb", NavasanSellKey: "thb_sell", NavasanBuyKey: "thb_buy", NavasanUnit: UnitToman},
	{ID: "JPY", Category: CategoryCurrency, NameFa: "ین ژاپن", Emoji: "💱", BonbastSellKey: "jpy1", BonbastBuyKey: "jpy2", BonbastUnit: UnitToman, NavasanKey: "jpy", NavasanSellKey: "jpy_sell", NavasanBuyKey: "jpy_buy", NavasanUnit: UnitToman},
	{ID: "SGD", Category: CategoryCurrency, NameFa: "دلار سنگاپور", Emoji: "💱", BonbastSellKey: "sgd1", BonbastBuyKey: "sgd2", BonbastUnit: UnitToman, NavasanKey: "sgd", NavasanSellKey: "sgd_sell", NavasanBuyKey: "sgd_buy", NavasanUnit: UnitToman},
	{ID: "HKD", Category: CategoryCurrency, NameFa: "دلار هنگ‌کنگ", Emoji: "💱", BonbastSellKey: "hkd1", BonbastBuyKey: "hkd2", BonbastUnit: UnitToman, NavasanKey: "hkd", NavasanSellKey: "hkd_sell", NavasanBuyKey: "hkd_buy", NavasanUnit: UnitToman},
	{ID: "NZD", Category: CategoryCurrency, NameFa: "دلار نیوزیلند", Emoji: "💱", BonbastSellKey: "nzd1", BonbastBuyKey: "nzd2", BonbastUnit: UnitToman, NavasanKey: "nzd", NavasanSellKey: "nzd_sell", NavasanBuyKey: "nzd_buy", NavasanUnit: UnitToman},
	{ID: "ZAR", Category: CategoryCurrency, NameFa: "رَند آفریقای جنوبی", Emoji: "💱", BonbastSellKey: "zar1", BonbastBuyKey: "zar2", BonbastUnit: UnitToman, NavasanKey: "zar", NavasanSellKey: "zar_sell", NavasanBuyKey: "zar_buy", NavasanUnit: UnitToman},
	{ID: "TRY", Category: CategoryCurrency, NameFa: "لیر ترکیه", Emoji: "💱", BonbastSellKey: "try1", BonbastBuyKey: "try2", BonbastUnit: UnitToman, NavasanKey: "try", NavasanSellKey: "try_sell", NavasanBuyKey: "try_buy", NavasanUnit: UnitToman},
	{ID: "CNY", Category: CategoryCurrency, NameFa: "یوان چین", Emoji: "💱", BonbastSellKey: "cny1", BonbastBuyKey: "cny2", BonbastUnit: UnitToman, NavasanKey: "cny", NavasanSellKey: "cny_sell", NavasanBuyKey: "cny_buy", NavasanUnit: UnitToman},
	{ID: "SAR", Category: CategoryCurrency, NameFa: "ریال عربستان", Emoji: "💱", BonbastSellKey: "sar1", BonbastBuyKey: "sar2", BonbastUnit: UnitToman, NavasanKey: "sar", NavasanSellKey: "sar_sell", NavasanBuyKey: "sar_buy", NavasanUnit: UnitToman},
	{ID: "INR", Category: CategoryCurrency, NameFa: "روپیه هند", Emoji: "💱", BonbastSellKey: "inr1", BonbastBuyKey: "inr2", BonbastUnit: UnitToman, NavasanKey: "inr", NavasanSellKey: "inr_sell", NavasanBuyKey: "inr_buy", NavasanUnit: UnitToman},
	{ID: "MYR", Category: CategoryCurrency, NameFa: "رینگیت مالزی", Emoji: "💱", BonbastSellKey: "myr1", BonbastBuyKey: "myr2", BonbastUnit: UnitToman, NavasanKey: "myr", NavasanSellKey: "myr_sell", NavasanBuyKey: "myr_buy", NavasanUnit: UnitToman},
	{ID: "DKK", Category: CategoryCurrency, NameFa: "کرون دانمارک", Emoji: "💱", BonbastSellKey: "dkk1", BonbastBuyKey: "dkk2", BonbastUnit: UnitToman, NavasanKey: "dkk", NavasanSellKey: "dkk_sell", NavasanBuyKey: "dkk_buy", NavasanUnit: UnitToman},
	{ID: "AED", Category: CategoryCurrency, NameFa: "درهم امارات", Emoji: "💱", BonbastSellKey: "aed1", BonbastBuyKey: "aed2", BonbastUnit: UnitToman, NavasanKey: "aed", NavasanSellKey: "aed_sell", NavasanBuyKey: "aed_buy", NavasanUnit: UnitToman},
	{ID: "IQD", Category: CategoryCurrency, NameFa: "دینار عراق", Emoji: "💱", BonbastSellKey: "iqd1", BonbastBuyKey: "iqd2", BonbastUnit: UnitToman, NavasanKey: "iqd", NavasanSellKey: "iqd_sell", NavasanBuyKey: "iqd_buy", NavasanUnit: UnitToman},
	{ID: "KWD", Category: CategoryCurrency, NameFa: "دینار کویت", Emoji: "💱", BonbastSellKey: "kwd1", BonbastBuyKey: "kwd2", BonbastUnit: UnitToman, NavasanKey: "kwd", NavasanSellKey: "kwd_sell", NavasanBuyKey: "kwd_buy", NavasanUnit: UnitToman},
	{ID: "BHD", Category: CategoryCurrency, NameFa: "دینار بحرین", Emoji: "💱", BonbastSellKey: "bhd1", BonbastBuyKey: "bhd2", BonbastUnit: UnitToman, NavasanKey: "bhd", NavasanSellKey: "bhd_sell", NavasanBuyKey: "bhd_buy", NavasanUnit: UnitToman},
	{ID: "OMR", Category: CategoryCurrency, NameFa: "ریال عمان", Emoji: "💱", BonbastSellKey: "omr1", BonbastBuyKey: "omr2", BonbastUnit: UnitToman, NavasanKey: "omr", NavasanSellKey: "omr_sell", NavasanBuyKey: "omr_buy", NavasanUnit: UnitToman},
	{ID: "QAR", Category: CategoryCurrency, NameFa: "ریال قطر", Emoji: "💱", BonbastSellKey: "qar1", BonbastBuyKey: "qar2", BonbastUnit: UnitToman, NavasanKey: "qar", NavasanSellKey: "qar_sell", NavasanBuyKey: "qar_buy", NavasanUnit: UnitToman},

	// -------- COINS --------
	{ID: "EMAMI", Category: CategoryCoin, NameFa: "سکه امامی", Emoji: "🪙", BonbastSellKey: "sekeb", BonbastUnit: UnitToman, NavasanKey: "sekeb", NavasanUnit: UnitToman},
	{ID: "BAHAR", Category: CategoryCoin, NameFa: "سکه بهار آزادی", Emoji: "🪙", BonbastSellKey: "sekeb1", BonbastUnit: UnitToman, NavasanKey: "sekeb1", NavasanUnit: UnitToman},
	{ID: "NIM", Category: CategoryCoin, NameFa: "نیم سکه", Emoji: "🪙", BonbastSellKey: "sekeb2", BonbastUnit: UnitToman, NavasanKey: "sekeb2", NavasanUnit: UnitToman},
	{ID: "ROB", Category: CategoryCoin, NameFa: "ربع سکه", Emoji: "🪙", BonbastSellKey: "sekeb3", BonbastUnit: UnitToman, NavasanKey: "sekeb3", NavasanUnit: UnitToman},
	{ID: "GERAMI", Category: CategoryCoin, NameFa: "سکه گرمی", Emoji: "🪙", BonbastSellKey: "sekeb4", BonbastUnit: UnitToman, NavasanKey: "sekeb4", NavasanUnit: UnitToman},

	// -------- GOLD --------
	{ID: "GERAM18", Category: CategoryGold, NameFa: "هر گرم طلای ۱۸ عیار", Emoji: "🥇", BonbastSellKey: "geram18", BonbastUnit: UnitToman, NavasanKey: "geram18", NavasanUnit: UnitToman},
	{ID: "GERAM24", Category: CategoryGold, NameFa: "هر گرم طلای ۲۴ عیار", Emoji: "🥇", BonbastSellKey: "geram24", BonbastUnit: UnitToman, NavasanKey: "geram24", NavasanUnit: UnitToman},
	{ID: "MITHQAL", Category: CategoryGold, NameFa: "مثقال طلا", Emoji: "🥇", BonbastSellKey: "mithqal", BonbastUnit: UnitToman, NavasanKey: "mithqal", NavasanUnit: UnitToman},
	{ID: "OUNCE", Category: CategoryGold, NameFa: "اونس طلا", Emoji: "🌍", BonbastSellKey: "gold", BonbastUnit: UnitUSD, NavasanKey: "usd_xau", NavasanUnit: UnitUSD},

	// -------- CRYPTO --------
	{ID: "BTC", Category: CategoryCrypto, NameFa: "بیت‌کوین", Emoji: "₿", BonbastSellKey: "btc", BonbastUnit: UnitUSD, NavasanKey: "btc", NavasanUnit: UnitUSD, NavasanIsCrypto: true},
}

var byID map[string]Item

func init() {
	byID = map[string]Item{}
	for _, it := range All {
		byID[it.ID] = it
	}
}

func ByID(id string) (Item, bool) {
	it, ok := byID[id]
	return it, ok
}

// Defaults returns the default enabled item IDs (requested by user).
func Defaults() []string {
	return []string{"USD", "EUR", "AED", "CAD", "TRY", "GBP", "EMAMI", "BAHAR", "NIM", "ROB", "GERAM18", "OUNCE", "BTC"}
}
