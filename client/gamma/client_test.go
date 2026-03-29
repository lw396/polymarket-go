package gamma

import (
	"log"
	"testing"

	"github.com/bytedance/sonic"
)

func TestGammaSDK_GetMarketByTokenId(t *testing.T) {
	tokenId := "25986405577356928223848081260299259484163711501323323218252464960086540660718"
	proxyUrl := "http://127.0.0.1:7890"
	client, err := NewGammaSDK(&proxyUrl)
	if err != nil {
		t.Error(err)
		return
	}
	query := &UpdatedMarketQuery{
		ClobTokenIDs: &tokenId,
	}
	res, err := client.GetMarkets(query)
	if err != nil {
		t.Fatal(err)
		return
	}
	resStr, err := sonic.MarshalString(res)
	if err != nil {
		t.Fatal(err)
		return
	}
	log.Println(resStr)
	log.Printf("conditionId:%v\n", res[0].ConditionID)
}

func TestGammaSDK_GetMarketBySlug(t *testing.T) {
	proxyUrl := "http://127.0.0.1:7890"
	client, err := NewGammaSDK(&proxyUrl)
	if err != nil {
		t.Error(err)
		return
	}
	slug := "eth-updown-15m-1770739200"
	market, err := client.GetMarketBySlug(slug, nil)
	if err != nil {
		t.Error(err)
		return
	}
	marketStr, err := sonic.MarshalString(market)
	if err != nil {

		t.Error(err)
		return
	}
	log.Println(marketStr)

}

func TestGetEvents(t *testing.T) {
	proxyUrl := "http://127.0.0.1:7890"
	client, err := NewGammaSDK(&proxyUrl)
	if err != nil {
		t.Error(err)
		return
	}

	query := &UpdatedEventQuery{
		Slug: &[]string{"btc-updown-5m-1774788300", "btc-updown-5m-1774788000"},
	}

	events, err := client.GetEvents(query)
	if err != nil {
		t.Error(err)
		return
	}

	eventsStr, err := sonic.MarshalString(events)
	if err != nil {

		t.Error(err)
		return
	}
	log.Println(eventsStr)

}
