module github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver

go 1.24.0

require (
	github.com/aws/aws-sdk-go-v2 v1.39.4
	github.com/aws/aws-sdk-go-v2/config v1.31.15
	github.com/aws/aws-sdk-go-v2/service/ecs v1.66.0
	github.com/docker/docker v28.5.1+incompatible
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil v0.139.0
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/common v0.139.0
	github.com/stretchr/testify v1.11.1
	go.opentelemetry.io/collector/component v1.45.0
	go.opentelemetry.io/collector/component/componenttest v0.139.0
	go.opentelemetry.io/collector/config/confighttp v0.139.0
	go.opentelemetry.io/collector/confmap v1.45.0
	go.opentelemetry.io/collector/confmap/xconfmap v0.139.0
	go.opentelemetry.io/collector/consumer v1.45.0
	go.opentelemetry.io/collector/consumer/consumertest v0.139.0
	go.opentelemetry.io/collector/pdata v1.45.0
	go.opentelemetry.io/collector/receiver v1.45.0
	go.opentelemetry.io/collector/receiver/receivertest v0.139.0
	go.opentelemetry.io/otel v1.40.0
	go.uber.org/goleak v1.3.0
	go.uber.org/zap v1.27.0
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.18.19 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.29.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.38.9 // indirect
	github.com/aws/smithy-go v1.23.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/foxboron/go-tpm-keyfiles v0.0.0-20250903184740-5d135037bd4d // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/go-tpm v0.9.6 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-version v1.7.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.1 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/knadh/koanf/providers/confmap v1.0.0 // indirect
	github.com/knadh/koanf/v2 v2.3.0 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/sys/atomicwriter v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/morikuni/aec v1.1.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rs/cors v1.11.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/collector/client v1.45.0 // indirect
	go.opentelemetry.io/collector/config/configauth v1.45.0 // indirect
	go.opentelemetry.io/collector/config/configcompression v1.45.0 // indirect
	go.opentelemetry.io/collector/config/configmiddleware v1.45.0 // indirect
	go.opentelemetry.io/collector/config/configopaque v1.45.0 // indirect
	go.opentelemetry.io/collector/config/configoptional v1.45.0 // indirect
	go.opentelemetry.io/collector/config/configtls v1.45.0 // indirect
	go.opentelemetry.io/collector/consumer/consumererror v0.139.0 // indirect
	go.opentelemetry.io/collector/consumer/xconsumer v0.139.0 // indirect
	go.opentelemetry.io/collector/extension/extensionauth v1.45.0 // indirect
	go.opentelemetry.io/collector/extension/extensionmiddleware v0.139.0 // indirect
	go.opentelemetry.io/collector/featuregate v1.45.0 // indirect
	go.opentelemetry.io/collector/pdata/pprofile v0.139.0 // indirect
	go.opentelemetry.io/collector/pipeline v1.45.0 // indirect
	go.opentelemetry.io/collector/receiver/xreceiver v0.139.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.40.0 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260128011058-8636f8732409 // indirect
	google.golang.org/grpc v1.78.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)

replace github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil => ../../internal/aws/ecsutil

replace github.com/open-telemetry/opentelemetry-collector-contrib/internal/common => ../../internal/common

retract (
	v0.76.2
	v0.76.1
	v0.65.0
)
