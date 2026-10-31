package feed

import (
	"context"
	"errors"
	"fmt"
	"io"

	pb "market_data_gateway/gen/exchange/v1"
)

type Handler interface {
	OnSnapshot(s *pb.BookSnapshot)
	OnEvent(e *pb.MarketEvent)
}

var ErrStreamEnded = errors.New("engine closed the feed stream")

func Run(ctx context.Context, client pb.EngineFeedClient, gatewayID string, h Handler) error {
	stream, err := client.Subscribe(ctx, &pb.FeedSubscribeRequest{GatewayId: gatewayID})
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	err = readLoop(stream.Recv, h)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func readLoop(recv func() (*pb.FeedMessage, error), h Handler) error {
	for {
		msg, err := recv()
		if errors.Is(err, io.EOF) {
			return ErrStreamEnded
		}
		if err != nil {
			return fmt.Errorf("feed stream: %w", err)
		}
		switch body := msg.GetBody().(type) {
		case *pb.FeedMessage_Snapshot:
			h.OnSnapshot(body.Snapshot)
		case *pb.FeedMessage_Event:
			h.OnEvent(body.Event)
		default:
		}
	}
}
