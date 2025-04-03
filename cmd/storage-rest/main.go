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
	"strings"
)

func main() {
	flagset := goflag.NewFlagSet("storage-rest", goflag.ContinueOnError)
	klog.InitFlags(flagset)

	st := os.Getenv("STORAGE_TYPE")
	storageType := types.BridgeStorageType(st)

	bs := storage.NewBridgeStorage(storageType)

	// setup ca.crt for TLS
	util.InClusterConfigHackForRHDHSidecars()

	r := strings.NewReplacer("\r", "", "\n", "")

	//TODO maybe change to LOCATION_URL
	bridgeURL := os.Getenv("BRIDGE_URL")
	bridgeURL = r.Replace(bridgeURL)
	cfg := &config.Config{}
	restCfg, err := util.GetK8sConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err.Error())
		os.Exit(1)
	}
	bridgeToken := util.GetCurrentToken(restCfg)
	bkstgURL := os.Getenv("BKSTG_URL")
	bkstgURL = r.Replace(bkstgURL)
	bkstgToken := os.Getenv("RHDH_TOKEN")
	bkstgToken = r.Replace(bkstgToken)

	podIP := os.Getenv("POD_IP")
	podIP = r.Replace(podIP)
	klog.Infof("pod IP from env var is %s", podIP)
	if len(podIP) > 0 {
		// neither inter-Pod nor service IPs worked for backstage access in testing; have to use the route
		bridgeURL = fmt.Sprintf("http://%s:9090", podIP)
	}

	server := storage.NewStorageRESTServer(bs, bridgeURL, bridgeToken, bkstgURL, bkstgToken)
	stopCh := util.SetupSignalHandler()
	server.Run(stopCh)

}
