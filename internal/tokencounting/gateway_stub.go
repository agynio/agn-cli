package tokencounting

import (
	"context"

	"google.golang.org/grpc"

	tokencountingv1 "github.com/agynio/agn-cli/internal/tokencounting/token_countingv1"
)

const (
	TokenCountingGatewayServiceName       = "agynio.api.gateway.v1.TokenCountingGateway"
	TokenCountingGatewayCountTokensMethod = "/agynio.api.gateway.v1.TokenCountingGateway/CountTokens"
)

type TokenCountingGatewayServer interface {
	CountTokens(context.Context, *tokencountingv1.CountTokensRequest) (*tokencountingv1.CountTokensResponse, error)
}

var tokenCountingGatewayServiceDesc = grpc.ServiceDesc{
	ServiceName: TokenCountingGatewayServiceName,
	HandlerType: (*TokenCountingGatewayServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CountTokens",
			Handler:    tokenCountingGatewayCountTokensHandler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "agynio/api/gateway/v1/token_counting.proto",
}

func RegisterTokenCountingGatewayServer(s grpc.ServiceRegistrar, srv TokenCountingGatewayServer) {
	s.RegisterService(&tokenCountingGatewayServiceDesc, srv)
}

func tokenCountingGatewayCountTokensHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(tokencountingv1.CountTokensRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TokenCountingGatewayServer).CountTokens(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: TokenCountingGatewayCountTokensMethod,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TokenCountingGatewayServer).CountTokens(ctx, req.(*tokencountingv1.CountTokensRequest))
	}
	return interceptor(ctx, in, info, handler)
}
