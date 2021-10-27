// This is a generated file. Do not edit directly.

module k8s.io/sample-cli-plugin

go 1.16

require (
	github.com/spf13/cobra v1.2.1
	github.com/spf13/pflag v1.0.5
	k8s.io/cli-runtime v0.0.0
	k8s.io/client-go v0.0.0
)

replace (
	github.com/golang/glog => github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	golang.org/x/net => golang.org/x/net v0.0.0-20210813160813-60bc85c4be6d
	golang.org/x/sys => golang.org/x/sys v0.0.0-20210820121016-41cdb8703e55
	golang.org/x/text => golang.org/x/text v0.3.6
	google.golang.org/genproto => google.golang.org/genproto v0.0.0-20210602131652-f16073e35f0c
	google.golang.org/grpc => google.golang.org/grpc v1.38.0
	google.golang.org/protobuf => google.golang.org/protobuf v1.26.0
	k8s.io/api => ../api
	k8s.io/apimachinery => ../apimachinery
	k8s.io/cli-runtime => ../cli-runtime
	k8s.io/client-go => ../client-go
	k8s.io/sample-cli-plugin => ../sample-cli-plugin
)
