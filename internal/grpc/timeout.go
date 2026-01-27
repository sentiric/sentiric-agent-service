// sentiric-agent-service/internal/grpc/timeout.go
// ✅ YENİ: gRPC Timeout Helper

package grpc

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

const DefaultGRPCTimeout = 3 * time.Second

// CallWithTimeout wraps a gRPC call with a 3-second timeout
// 
// Example usage:
//   response, err := CallWithTimeout(ctx, func(ctx context.Context) (*pb.Response, error) {
//       return client.SomeMethod(ctx, request)
//   })
func CallWithTimeout[T any](
	parentCtx context.Context,
	fn func(context.Context) (T, error),
) (T, error) {
	ctx, cancel := context.WithTimeout(parentCtx, DefaultGRPCTimeout)
	defer cancel()
	
	result, err := fn(ctx)
	
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Warn().Msg("⏱️ gRPC call timeout (3s)")
		}
	}
	
	return result, err
}

// CallWithCustomTimeout allows custom timeout duration
func CallWithCustomTimeout[T any](
	parentCtx context.Context,
	timeout time.Duration,
	fn func(context.Context) (T, error),
) (T, error) {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	
	return fn(ctx)
}
