package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"time"

	pb "gocar/proto" // your generated proto package

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	carId     = "C"
	authToken = "demo-token-C"
	inputRate = time.Second / 60 // 60 FPS
)

func getServerAddr() string {
	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = "localhost:50051" // fallback for local development
	}
	return addr
}

type CarClient struct {
	client     pb.CarServiceClient
	conn       *grpc.ClientConn
	sequence   int32
	myCarState *pb.CarState
}

func NewCarClient() (*CarClient, error) {
	// Connect to server
	serverAddr := getServerAddr()
	log.Printf("Attempting to connect to: %s", serverAddr)

	conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %v", err)
	}

	client := pb.NewCarServiceClient(conn)

	return &CarClient{
		client:   client,
		conn:     conn,
		sequence: 0,
	}, nil
}

func (c *CarClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// Stream race updates in background
func (c *CarClient) streamUpdates(ctx context.Context) error {
	stream, err := c.client.StreamRaceUpdates(ctx, &pb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to stream updates: %v", err)
	}

	for {
		update, err := stream.Recv()
		if err == io.EOF {
			log.Println("Stream ended")
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream error: %v", err)
		}

		c.handleUpdate(update)
	}
}

func (c *CarClient) handleUpdate(update *pb.RaceUpdate) {
	switch u := update.Update.(type) {
	case *pb.RaceUpdate_CheckIn:
		log.Println("=== CHECK IN ===")
		for _, car := range u.CheckIn.Cars {
			log.Printf("Car %s: %s (Driver: %s, Team: %s, Power: %.0f)",
				car.CarId, car.Color, car.Driver, car.Team, car.Power)
		}
		log.Println("================")

	case *pb.RaceUpdate_RaceData:
		// Find our car state
		for _, car := range u.RaceData.Cars {
			if car.CarId == carId {
				c.myCarState = car
				log.Printf("🏎️  Car C | Position: (%.1f, %.1f) | Speed: %.1f | Heading: %.1f° | Lap: %d/%d",
					car.X, car.Y, car.Speed, car.Heading, car.Lap, u.RaceData.RaceStatus.TotalLaps)
				break
			}
		}

		// Show race status
		status := u.RaceData.RaceStatus
		if status.Status == "finished" {
			log.Printf("🏁 RACE FINISHED! Leader: Car %s | Time: %.2fs",
				status.LeaderCarId, float64(status.RaceTime)/1000.0)
		}
	}
}

// Send player input
func (c *CarClient) sendInput(ctx context.Context, steering, throttle, brake float32, boost bool) error {
	c.sequence++

	input := &pb.PlayerInput{
		CarId:     carId,
		AuthToken: authToken,
		Steering:  steering,
		Throttle:  throttle,
		Brake:     brake,
		Boost:     boost,
		Timestamp: time.Now().UnixMilli(),
		Sequence:  c.sequence,
	}

	ack, err := c.client.SendPlayerInput(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to send input: %v", err)
	}

	if !ack.Accepted {
		return fmt.Errorf("input rejected: %s", ack.Reason)
	}

	return nil
}

// Simple AI driver
func (c *CarClient) getAIInput() (steering, throttle, brake float32, boost bool) {
	// Simple AI: mostly go straight with occasional turns
	throttle = 1.0 // Always accelerate

	// Occasionally turn
	if rand.Float32() < 0.1 {
		steering = (rand.Float32() - 0.5) * 0.5 // Small random turns
	}

	// Occasionally boost
	boost = rand.Float32() < 0.05

	// Brake if we're about to go off track (top or bottom)
	if c.myCarState != nil {
		if c.myCarState.Y < 50 || c.myCarState.Y > 450 {
			brake = 0.3
			throttle = 0.3
		}
	}

	return
}

func (c *CarClient) runInputLoop(ctx context.Context) {
	ticker := time.NewTicker(inputRate)
	defer ticker.Stop()

	log.Println("🎮 Starting AI driver for Car C...")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			steering, throttle, brake, boost := c.getAIInput()

			if err := c.sendInput(ctx, steering, throttle, brake, boost); err != nil {
				log.Printf("Input error: %v", err)
			}
		}
	}
}

func main() {
	serverAddr := getServerAddr()
	log.Printf("🏎️  Car C Client starting...")
	log.Printf("Connecting to server at %s", serverAddr)

	client, err := NewCarClient()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Start streaming updates
	go func() {
		if err := client.streamUpdates(ctx); err != nil {
			log.Printf("Stream error: %v", err)
		}
	}()

	// Wait a moment for check-in
	time.Sleep(500 * time.Millisecond)

	// Start sending input
	client.runInputLoop(ctx)
}
