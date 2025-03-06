package storage

import (
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/rest"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"net/http"
	"strings"
)

type BridgeStorageRESTClient struct {
	RESTClient       *resty.Client
	UpsertURL        string
	CurrentKeySetURL string
	Token            string
}

func SetupBridgeStorageRESTClient(hostURL, token string) *BridgeStorageRESTClient {
	b := &BridgeStorageRESTClient{
		RESTClient:       resty.New(),
		UpsertURL:        hostURL + util.UpsertURI,
		CurrentKeySetURL: hostURL + util.CurrenKeySetURI,
		Token:            token,
	}
	return b
}

func (b *BridgeStorageRESTClient) UpsertModel(importKey string, buf []byte) (int, string, *rest.PostBody, error) {
	var err error
	var storageResp *resty.Response
	body := rest.PostBody{
		Body: buf,
	}
	storageResp, err = b.RESTClient.R().SetBody(body).SetAuthToken(b.Token).SetQueryParam(util.KeyQueryParam, importKey).SetHeader("Accept", "application/json").Post(b.UpsertURL)
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
