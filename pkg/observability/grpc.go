package observability

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc/stats"
)

// ExcludeGRPCMethods returns a filter that disables automatic recording for
// the listed full method names. otelgrpc still injects or extracts propagation
// headers when this filter returns false.
func ExcludeGRPCMethods(fullMethods ...string) otelgrpc.Filter {
	excluded := make(map[string]struct{}, len(fullMethods))
	for _, method := range fullMethods {
		excluded[method] = struct{}{}
	}
	return func(info *stats.RPCTagInfo) bool {
		if info == nil {
			return true
		}
		_, skip := excluded[info.FullMethodName]
		return !skip
	}
}
