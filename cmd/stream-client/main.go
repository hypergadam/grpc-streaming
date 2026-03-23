package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	httpUnaryDemo()
}

// ▎ Plain HTTP GET. The gateway receives this, translates it to a gRPC GetDeviceInfo call with uuid="device-abc-123" (extracted from the URL path), gets the response, and writes it back as
// JSON. Zero gRPC knowledge required on the HTTP side.
func httpUnaryDemo() {
	resp, err := http.Get("http://localhost:8092/v1/wearable/device-abc-123/info")
	if err != nil {
		log.Fatalf("failed to get device info: %v", err.Error())
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("failed to read response body: %v", err.Error())
	}

	log.Printf("Device Info: %s\n", string(body))
}

//  The gateway uses chunked HTTP transfer encoding for server streaming — it keeps the HTTP connection open and flushes each gRPC stream message as a JSON line. The client reads the body   
//   line by line. When you break out of the loop and resp.Body.Close() runs via defer, the HTTP connection closes, the gateway cancels the gRPC stream, and the server's stream.Context().Done()
//    fires — the goroutine exits cleanly. This is the full cancellation chain.  
func httpServerStreamingDemo() {
	resp, err := http.Get("http://localhost:8092/v1/wearable/device-abc-123/beats-per-minute")
	if err != nil {
		log.Fatalf("failed to get beats per minute: %v", err.Error())
	}

	defer resp.Body.Close()

	// the gateway sends each stream message as a separate json line
	// bufio.Scanner reads line by line - each line is one BPM message
	scanner := bufio.NewScanner(resp.Body)
	count := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var result map[string]any
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			log.Printf("failed to unmarshal text: %v", err.Error())
			continue
		}
		fmt.Printf("[HTTP STREAM] %v\n", result)
		count++

		if count > 6 {
			break // read 5 messages then stop.
		}
	}
}
