package storage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/rest"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
)

type BridgeStorageRESTClient struct {
	RESTClient       *resty.Client
	UpsertURL        string
	CurrentKeySetURL string
	ListURL          string
	FetchURL         string
	Token            string
}

func SetupBridgeStorageRESTClient(hostURL, token string) *BridgeStorageRESTClient {
	b := &BridgeStorageRESTClient{
		RESTClient:       resty.New(),
		UpsertURL:        hostURL + util.UpsertURI,
		CurrentKeySetURL: hostURL + util.CurrentKeySetURI,
		ListURL:          hostURL + util.ListURI,
		FetchURL:         hostURL + util.FetchURI,
		Token:            token,
	}
	return b
}

func (b *BridgeStorageRESTClient) UpsertModel(importKey, normalizerType, lastUpdateTimeSinceEpoch, modelCardKey string, modelCard *string, buf []byte) (int, string, *rest.PostBody, error) {
	var err error
	var storageResp *resty.Response
	body := rest.PostBody{
		Body:                     buf,
		LastUpdateTimeSinceEpoch: lastUpdateTimeSinceEpoch,
	}
    r := strings.NewReplacer(" ", "")
	if modelCard != nil {
		body.ModelCard = *modelCard
		body.ModelCardKey = r.Replace(modelCardKey)
	}
	storageResp, err = b.RESTClient.R().SetBody(body).SetAuthToken(b.Token).SetQueryParam(util.KeyQueryParam, importKey).SetQueryParam(util.TypeQueryParam, normalizerType).SetHeader("Accept", "application/json").Post(b.UpsertURL)
	msg := fmt.Sprintf("%#v", storageResp)
	if err != nil {
		return http.StatusInternalServerError, msg, &body, err
	}
	return storageResp.StatusCode(), msg, &body, nil
}

func (b *BridgeStorageRESTClient) PostCurrentKeySet(keys []string) (int, string, error) {
	var err error
	var storageResp *resty.Response

	qp := strings.Join(keys, ",")
	storageResp, err = b.RESTClient.R().SetAuthToken(b.Token).SetQueryParam(util.KeyQueryParam, qp).SetHeader("Accept", "application/json").Post(b.CurrentKeySetURL)
	msg := fmt.Sprintf("%#v", storageResp)
	if err != nil {
		return http.StatusInternalServerError, msg, err
	}

	return storageResp.StatusCode(), msg, nil
}

func (b *BridgeStorageRESTClient) ListModelsKeys() (int, string, error, []string) {
	var err error
	var storageResp *resty.Response

	storageResp, err = b.RESTClient.R().SetAuthToken(b.Token).SetHeader("Accept", "application/json").Get(b.ListURL)
	msg := fmt.Sprintf("%#v", storageResp)
	if err != nil {
		return http.StatusInternalServerError, msg, err, []string{}
	}

	d := &DiscoverResponse{}
	err = json.Unmarshal(storageResp.Body(), d)
	if err != nil {
		return http.StatusBadRequest, msg, err, []string{}
	}

	return storageResp.StatusCode(), msg, nil, d.Keys
}

func (b *BridgeStorageRESTClient) FetchModel(key string) (int, string, error, []byte) {
	var err error
	var storageResp *resty.Response

	storageResp, err = b.RESTClient.R().SetAuthToken(b.Token).SetQueryParam(util.KeyQueryParam, key).SetHeader("Accept", "application/json").Get(b.FetchURL)
	msg := fmt.Sprintf("%#v", storageResp)
	if err != nil {
		return http.StatusInternalServerError, msg, err, []byte{}
	}

	return storageResp.StatusCode(), msg, nil, storageResp.Body()
}
