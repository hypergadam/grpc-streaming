package main

import (
	"context"
	"errors"
	"io"
	"log"
	"time"

	wearablepb "github.com/hypergadam/grpc-streaming/pkg/api/wearable/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	conn, err := grpc.NewClient(
		"localhost:9879",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to connect: %v", err.Error())
	}

	defer conn.Close()

	client := wearablepb.NewWearableServiceClient(conn)

	unaryDemo(client)
	serverStreamingDemo(client)
}

// TYPE 1 - Unary Demo
func unaryDemo(client wearablepb.WearableServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetDeviceInfo(ctx, &wearablepb.GetDeviceInfoRequest{
		Uuid: "device-abc-123",
	})
	if err != nil {
		log.Fatalf("failed to get device info: %v", err.Error())
	}

	log.Printf("Device Info: %+v\n", resp)
}

// TYPE 2 - Server streaming
func serverStreamingDemo(client wearablepb.WearableServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.BeatsPerMinute(ctx, &wearablepb.BeatsPerMinuteRequest{
		Uuid: "device-abc-123",
	})
	if err != nil {
		log.Fatalf("failed to get beats per minute: %v", err.Error())
	}

	for {
		resp, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			log.Fatalf("failed to receive beats per minute: %v", err.Error())
		}

		log.Printf("Beats Per Minute: %+v\n", resp)
	}
}

// TYPE 3 - Client streaming
func clientStreamingDemo(client wearablepb.WearableServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.UploadHealthData(ctx)
	if err != nil {
		log.Fatalf("UploadHealthData error: %v", err.Error())
	}

	points := []uint32{72, 75, 68, 80, 77, 73, 69, 82, 76, 21}
	for idx := range points {
		bpm := points[idx]
		// stream.Send() pushes one data point to the server — the server's Recv() unblocks. stream.CloseAndRecv() tells the server "I'm done sending" (triggers io.EOF on server's Recv()) and then
		// waits for the server's one response. This is the client-side mirror of SendAndClose on the server.
		if err := stream.Send(&wearablepb.HealthDataPoint{
			Uuid:      "device-abc-123",
			Bpm:       bpm,
			Timestamp: time.Now().Unix(),
		}); err != nil {
			log.Fatalf("failed to send health data point: %v", err.Error())
		}
		log.Printf("[CLIENT STREAM] Sent health data point: %+v\n", bpm)
		time.Sleep(300 * time.Microsecond)
	}

	summary, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("failed to receive health data summary: %v", err.Error())
	}

	log.Printf("[CLIENT STREAM]Health Data Summary: %+v\n", summary)
}

// TYPE 4 - Bidirectional Streaming
func bidirectionalStreamingDemo(client wearablepb.WearableServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := client.LiveMonitor(ctx)
	if err != nil {
		log.Fatalf("LiveMonitor error: %v", err.Error())
	}

	// Send commands in a separate goroutine so we can receive
	// responses concurrently - this is the key bidi streaming pattern.
	go func() {
		commands := []wearablepb.LiveMonitorRequest_Command{
			wearablepb.LiveMonitorRequest_START,
			wearablepb.LiveMonitorRequest_PAUSE,
			wearablepb.LiveMonitorRequest_START,
			wearablepb.LiveMonitorRequest_STOP,
		}

		for idx := range commands {
			cmd := commands[idx]

			if err := stream.Send(&wearablepb.LiveMonitorRequest{Command: cmd}); err != nil {
				log.Printf("[BIDI] send error: %v", err.Error())
				return
			}

			log.Printf("[BIDI] sent command: %+v", cmd)
			time.Sleep(time.Second)
		}

		if err := stream.CloseSend(); err != nil {
			log.Printf("[BIDI] close send error: %v", err.Error())
		}
	}()

	// Receive responses from main goroutine.
	for {
		resp, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Printf("[BIDI] stream closed by server")
				break
			}

			log.Printf("[BIDI] receive error: %v", err.Error())
			return
		}

		log.Printf("[BIDI] status: %s, bpm: %d, message: %s", resp.Status, resp.Bpm, resp.Message)
	}
}

// NOTE:   ▎ The goroutine for sending and the main loop for receiving run concurrently — this is the essence of bidi streaming. Neither side waits for the other. stream.CloseSend() closes only the
// client's send side — the server can still send after this. The receive loop ends when the server closes its side (io.EOF) or an error occurs