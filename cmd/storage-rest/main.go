package main

import (
	goflag "flag"
	"fmt"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/server/storage"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/config"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"k8s.io/klog/v2"
	"os"
)

func main() {
	flagset := goflag.NewFlagSet("storage-rest", goflag.ContinueOnError)
	klog.InitFlags(flagset)

	st := os.Getenv("STORAGE_TYPE")
	storageType := types.BridgeStorageType(st)

	bs := storage.NewBridgeStorage(storageType)

	//TODO maybe change to LOCATION_URL
	bridgeURL := os.Getenv("BRIDGE_URL")
	cfg := &config.Config{}
	restCfg, err := util.GetK8sConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err.Error())
		os.Exit(1)
	}
	bridgeToken := util.GetCurrentToken(restCfg)
	bkstgURL := os.Getenv("BKSTG_URL")
	bkstgToken := os.Getenv("RHDH_TOKEN")

	nfstr := os.Getenv(types.FormatEnvVar)
	nf := types.NormalizerFormat(nfstr)

	server := storage.NewStorageRESTServer(bs, bridgeURL, bridgeToken, bkstgURL, bkstgToken, nf)
	stopCh := util.SetupSignalHandler()
	server.Run(stopCh)

}
