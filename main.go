package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"market_data_gateway/feed"
	pb "market_data_gateway/gen/exchange/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() { os.Exit(run()) }

func run() int {
	cfg := LoadConfig()

	conn, err := grpc.NewClient(cfg.EngineFeed, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("engine feed client failed", "err", err)
		return 1
	}
	defer conn.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("market data gateway started", "engine_feed", cfg.EngineFeed, "gateway_id", cfg.GatewayID)
	err = feed.Run(ctx, pb.NewEngineFeedClient(conn), cfg.GatewayID, logHandler{})
	if errors.Is(err, context.Canceled) {
		slog.Info("stopped")
		return 0
	}

	slog.Error("feed stopped", "err", err)
	return 1
}

type logHandler struct{}

func (logHandler) OnSnapshot(s *pb.BookSnapshot) {
	slog.Info("snapshot", "symbol", s.GetSymbol(), "as_of_seq", s.GetAsOfSeq(),
		"bids", len(s.GetBids()), "asks", len(s.GetAsks()), "halted", s.GetHalted())
}

func (logHandler) OnEvent(e *pb.MarketEvent) {
	slog.Info("event", "symbol", e.GetSymbol(), "seq", e.GetSeq(), "kind", e.GetKind().String(),
		"order_id", e.GetOrderId(), "passive_id", e.GetPassiveId(), "side", e.GetSide().String(),
		"price", e.GetPrice(), "qty", e.GetQuantity())
}
