package main

import (
	goflag "flag"
	"fmt"
	gin_gonic_http_srv "github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/server/location/server"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"k8s.io/klog/v2"
	"os"
	"strings"
)

func main() {
	flagset := goflag.NewFlagSet("location", goflag.ContinueOnError)
	klog.InitFlags(flagset)

	st := os.Getenv("STORAGE_URL")
	rr := strings.NewReplacer("\r", "", "\n", "")
	st = rr.Replace(st)
	if len(st) == 0 {
		// try our RHDH sidecar container hack
		podIP := os.Getenv("POD_IP")
		st = fmt.Sprintf("http://%s:7070", podIP)
		klog.Infof("using %s for the storage URL per our sidecar hack", st)
	}
	server := gin_gonic_http_srv.NewImportLocationServer(st)
	stopCh := util.SetupSignalHandler()
	server.Run(stopCh)

}
