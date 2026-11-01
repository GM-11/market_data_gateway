package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"market_data_gateway/feed"
	pb "market_data_gateway/gen/exchange/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	cfg := LoadConfig()

	conn, err := grpc.NewClient(cfg.EngineFeed, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("engine feed client failed", "err", err)
		return
	}
	defer conn.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("market data gateway started", "engine_feed", cfg.EngineFeed, "gateway_id", cfg.GatewayID)
	err = feed.Run(ctx, pb.NewEngineFeedClient(conn), cfg.GatewayID, logHandler{})
	if errors.Is(err, context.Canceled) {
		slog.Info("stopped")
		os.Exit(0)
	}

	slog.Error("feed stopped", "err", err)

	mux := http.NewServeMux()

	fmt.Println("Server started on :8082")
	log.Fatal(http.ListenAndServe(":8082", mux))

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
