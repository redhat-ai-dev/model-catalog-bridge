package storage

import (
     "github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/server/storage/configmap"
     "github.com/redhat-ai-dev/model-catalog-bridge/pkg/config"
     "github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
     "github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
)

func NewBridgeStorage(storageType types.BridgeStorageType) types.BridgeStorage {
     switch storageType {
     default:
          fallthrough
     case types.ConfigMapBridgeStorage:
          st := configmap.ConfigMapBridgeStorage{}
          cfg, err := util.GetK8sConfig(&config.Config{})
          if err != nil {
               return nil
          }
          st.Initialize(cfg)
          return &st
     case types.GithubBridgeStorage:
     }
     return nil
}
