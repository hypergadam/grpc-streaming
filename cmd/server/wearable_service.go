package main

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"time"

	wearablepb "github.com/hypergadam/grpc-streaming/pkg/api/wearable/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type wearableServer struct {
	wearablepb.UnimplementedWearableServiceServer
}

// STREAMING TYPES

// TYPE 1 - Unary; signature => (ctx, *req) (*res, error)
// One request → One response. Classic HTTP-style call.
// The context.Context carries metadata, cancellation signals, and deadlines.
func (w *wearableServer) GetDeviceInfo(
	_ context.Context,
	req *wearablepb.GetDeviceInfoRequest,
) (*wearablepb.GetDeviceInfoResponse, error) {
	return &wearablepb.GetDeviceInfoResponse{
		Uuid:              req.Uuid,
		Name:              "SmartWatch",
		BatteryPercentage: 80,
		FirmwareVersion:   "1.0.0",
	}, nil
}

// TYPE 2 - Server Streaming; signature => (*req, stream) error
// One request → stream of responses.
// The function never returns until the client disconnects or decide to stop.
func (w *wearableServer) BeatsPerMinute(
	req *wearablepb.BeatsPerMinuteRequest,
	stream wearablepb.WearableService_BeatsPerMinuteServer,
) error {
	for {
		select {
		case <-stream.Context().Done(): // without it, this goroutine leaks forever when the client disconnects.
			return status.Error(codes.Canceled, "client disconnected")
		default:
			time.Sleep(time.Second)
			bpm := 60 + rand.Int31n(40)

			if err := stream.Send(&wearablepb.BeatsPerMinuteResponse{
				Value:  uint32(bpm),
				Minute: uint32(time.Now().Minute()),
			}); err != nil {
				return status.Error(codes.Canceled, "stream send failed")
			}
		}
	}
}

// TYPE 3 - Client streaming; signature => (stream) error
// Stream of requests → One response.
// Use case: wearable uploads a batch of health data collected offline.
// Server processes all of it and returns a single summary at the end.
// When we got io.EOF, it means the client has finished sending data.
func (w *wearableServer) UploadHealthData(
	stream wearablepb.WearableService_UploadHealthDataServer,
) error {
	var (
		total, sum uint32
		minBpm     uint32 = ^uint32(0)
		maxBpm     uint32
	)

	for {
		point, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				var avg uint32
				if total > 0 {
					avg = sum / total
				}

				return stream.SendAndClose(&wearablepb.HealthDataSummary{
					TotalPoints: total,
					AvgBpm:      avg,
					MinBpm:      minBpm,
					MaxBpm:      maxBpm,
				})
			}

			return status.Errorf(codes.Internal, "receiving data error: %v", err.Error())
		}

		total++
		sum += point.Bpm

		if point.Bpm < minBpm {
			minBpm = point.Bpm
		}

		if point.Bpm > maxBpm {
			maxBpm = point.Bpm
		}
	}
}

// TYPE 4 - Bidirectional Streaming; signature => (stream) error
// Bidirectional streaming allows the client and server to send messages to each other
// simultaneously.
// Use case: live interactive session. Client sends commands (START/PAUSE/STOP),
// server streams real-time readings back while active.
// Both Recv() and Send() can be called at any time.
func (w *wearableServer) LiveMonitor(
	stream wearablepb.WearableService_LiveMonitorServer,
) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return status.Errorf(codes.Internal, "receiving data error: %v", err.Error())
		}

		switch req.Command {
		case wearablepb.LiveMonitorRequest_START:
			if err := stream.Send(&wearablepb.LiveMonitorResponse{
				Status:  "MONITORING",
				Bpm:     uint32(60 + rand.Int31n(40)),
				Message: "Live monitoring started",
			}); err != nil {
				return status.Errorf(codes.Internal, "sending data error: %v", err.Error())
			}
		case wearablepb.LiveMonitorRequest_PAUSE:
			if err := stream.Send(&wearablepb.LiveMonitorResponse{
				Status:  "PAUSED",
				Bpm:     0,
				Message: "Live monitoring paused",
			}); err != nil {
				return status.Errorf(codes.Internal, "sending data error: %v", err.Error())
			}
		case wearablepb.LiveMonitorRequest_STOP:
			_ = stream.Send(&wearablepb.LiveMonitorResponse{ // we ignore the error because we are about to close the stream anyway.
				Status:  "STOPPED",
				Bpm:     0,
				Message: "Live monitoring stopped",
			})
			return nil
		}
	}
}
