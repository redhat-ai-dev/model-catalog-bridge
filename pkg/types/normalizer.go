package types

type NormalizerFormat string

const (
     CatalogInfoYamlFormat NormalizerFormat = "CatalogInfoYamlFormat"
     JsonArrayForamt       NormalizerFormat = "JsonArrayFormat"

     FormatEnvVar = "NORMALIZER_FORMAT"
)
