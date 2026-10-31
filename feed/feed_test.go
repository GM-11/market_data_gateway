package feed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	pb "market_data_gateway/gen/exchange/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// recorder is a Handler that writes down what it was given, in order.
type recorder struct{ got []string }

func (r *recorder) OnSnapshot(s *pb.BookSnapshot) {
	r.got = append(r.got, fmt.Sprintf("snap:%d@%d", s.GetSymbol(), s.GetAsOfSeq()))
}
func (r *recorder) OnEvent(e *pb.MarketEvent) {
	r.got = append(r.got, fmt.Sprintf("event:%d#%d", e.GetSymbol(), e.GetSeq()))
}

func snapMsg(symbol uint32, asOf uint64) *pb.FeedMessage {
	return &pb.FeedMessage{Body: &pb.FeedMessage_Snapshot{Snapshot: &pb.BookSnapshot{Symbol: symbol, AsOfSeq: asOf}}}
}
func eventMsg(symbol uint32, seq uint64) *pb.FeedMessage {
	return &pb.FeedMessage{Body: &pb.FeedMessage_Event{Event: &pb.MarketEvent{Symbol: symbol, Seq: seq}}}
}

// fakeRecv returns msgs one by one, then end.
func fakeRecv(end error, msgs ...*pb.FeedMessage) func() (*pb.FeedMessage, error) {
	i := 0
	return func() (*pb.FeedMessage, error) {
		if i < len(msgs) {
			i++
			return msgs[i-1], nil
		}
		return nil, end
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Given a snapshot and events for two symbols, when read, then the handler
// gets each one, in the same order, as the right kind of call.
func TestReadLoopDeliversInOrder(t *testing.T) {
	r := &recorder{}
	err := readLoop(fakeRecv(io.EOF,
		snapMsg(1, 100), snapMsg(2, 0), eventMsg(1, 101), eventMsg(2, 1), eventMsg(1, 102)), r)
	want := []string{"snap:1@100", "snap:2@0", "event:1#101", "event:2#1", "event:1#102"}
	if !equal(r.got, want) {
		t.Fatalf("got %v, want %v", r.got, want)
	}
	if !errors.Is(err, ErrStreamEnded) {
		t.Fatalf("err = %v, want ErrStreamEnded", err)
	}
}

// Given the stream fails, then readLoop stops and returns that error.
func TestReadLoopReturnsStreamError(t *testing.T) {
	boom := status.Error(codes.ResourceExhausted, "fell behind; subscribe again for a fresh snapshot")
	r := &recorder{}
	err := readLoop(fakeRecv(boom, eventMsg(1, 1)), r)
	if status.Code(errors.Unwrap(err)) != codes.ResourceExhausted {
		t.Fatalf("err = %v, want the stream's ResourceExhausted", err)
	}
	if len(r.got) != 1 {
		t.Fatalf("handler got %v, want the one event before the error", r.got)
	}
}

// Given a message with no body, then it is skipped and reading goes on.
func TestReadLoopSkipsEmptyMessage(t *testing.T) {
	r := &recorder{}
	_ = readLoop(fakeRecv(io.EOF, &pb.FeedMessage{}, eventMsg(1, 5)), r)
	if !equal(r.got, []string{"event:1#5"}) {
		t.Fatalf("got %v", r.got)
	}
}

// --- Run against a real in-process gRPC server ---

type fakeEngine struct {
	pb.UnimplementedEngineFeedServer
	gotReq chan *pb.FeedSubscribeRequest
	send   []*pb.FeedMessage
	hold   bool // keep the stream open after sending
}

func (f *fakeEngine) Subscribe(req *pb.FeedSubscribeRequest, s grpc.ServerStreamingServer[pb.FeedMessage]) error {
	f.gotReq <- req
	for _, m := range f.send {
		if err := s.Send(m); err != nil {
			return err
		}
	}
	if f.hold {
		<-s.Context().Done()
	}
	return nil
}

func startEngine(t *testing.T, f *fakeEngine) pb.EngineFeedClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterEngineFeedServer(srv, f)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewEngineFeedClient(conn)
}

// Given an engine, when Run subscribes, then it sends our gateway id, asks
// for every symbol (empty list) with no from_seq, and passes messages through.
// When the engine ends the stream, Run returns ErrStreamEnded.
func TestRunSubscribesToAllSymbols(t *testing.T) {
	f := &fakeEngine{gotReq: make(chan *pb.FeedSubscribeRequest, 1),
		send: []*pb.FeedMessage{snapMsg(1, 7), eventMsg(1, 8)}}
	client := startEngine(t, f)

	r := &recorder{}
	err := Run(context.Background(), client, "md-test", r)

	req := <-f.gotReq
	if req.GetGatewayId() != "md-test" || len(req.GetSymbols()) != 0 || len(req.GetFromSeq()) != 0 {
		t.Fatalf("request = %v, want gateway md-test, all symbols, no from_seq", req)
	}
	if !equal(r.got, []string{"snap:1@7", "event:1#8"}) {
		t.Fatalf("got %v", r.got)
	}
	if !errors.Is(err, ErrStreamEnded) {
		t.Fatalf("err = %v, want ErrStreamEnded", err)
	}
}

// Given the stream is open, when ctx is cancelled (Ctrl-C), then Run returns
// context.Canceled, not a stream error.
func TestRunStopsOnCancel(t *testing.T) {
	f := &fakeEngine{gotReq: make(chan *pb.FeedSubscribeRequest, 1), hold: true}
	client := startEngine(t, f)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, client, "md-test", &recorder{}) }()
	<-f.gotReq
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

// Given no engine is running, then Run returns an error instead of hanging.
func TestRunFailsWhenEngineDown(t *testing.T) {
	conn, err := grpc.NewClient("127.0.0.1:1", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = Run(ctx, pb.NewEngineFeedClient(conn), "md-test", &recorder{})
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a fast Unavailable error", err)
	}
}
