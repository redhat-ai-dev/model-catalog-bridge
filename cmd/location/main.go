package main

import (
	goflag "flag"
	"fmt"
	gin_gonic_http_srv "github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/server/location/server"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"k8s.io/klog/v2"
	"os"
	"strings"
)

func main() {
	var address string
	goflag.StringVar(&address, "address", "9090", "The port the location service listens on.")
	flagset := goflag.NewFlagSet("location", goflag.ContinueOnError)
	flagset.Parse(goflag.CommandLine.Args())
	klog.InitFlags(flagset)
	goflag.Parse()

	st := os.Getenv(types.StorageUrlEnvVar)
	rr := strings.NewReplacer("\r", "", "\n", "")
	st = rr.Replace(st)
	if len(st) == 0 {
		// try our RHDH sidecar container hack
		podIP := os.Getenv(util.PodIPEnvVar)
		st = fmt.Sprintf("http://%s:7070", podIP)
		klog.Infof("using %s for the storage URL per our sidecar hack", st)
	}
	nfstr := os.Getenv(types.FormatEnvVar)
	nfstr = rr.Replace(nfstr)
	nf := types.NormalizerFormat(nfstr)
	if len(nfstr) == 0 {
		nf = types.JsonArrayForamt
	}
	server := gin_gonic_http_srv.NewImportLocationServer(st, address, nf)
	stopCh := util.SetupSignalHandler()
	server.Run(stopCh)

}
