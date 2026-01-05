package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"time"

	pb "gocar/proto" // your generated proto package

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	carId           = "C"
	authToken       = "demo-token-C"
	inputRate       = time.Second / 60 // 60 updates per second
	lookAheadPoints = 8                // how many centerline points to look ahead
	maxOffTrackDist = 40.0             // consider off-track if farther than this (tune)
)

func getServerAddr() string {
	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = "localhost:50051" // fallback for local dev
	}
	return addr
}

type CarClient struct {
	client     pb.CarServiceClient
	conn       *grpc.ClientConn
	sequence   int32 // kept for local logging/debug, not sent
	myCarState *pb.CarState
	centerline []Point // computed centerline points
}

type Point struct {
	X, Y float64
}

func NewCarClient() (*CarClient, error) {
	serverAddr := getServerAddr()
	log.Printf("Connecting to %s...", serverAddr)

	conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial failed: %v", err)
	}

	return &CarClient{
		client: pb.NewCarServiceClient(conn),
		conn:   conn,
	}, nil
}

func (c *CarClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// Fetch track boundaries and compute simple centerline
func (c *CarClient) loadTrack(ctx context.Context) error {
	track, err := c.client.GetTrack(ctx, &pb.Empty{})
	if err != nil {
		return fmt.Errorf("GetTrack failed: %v", err)
	}

	log.Printf("Loaded track: %s (%s) — %d left / %d right points",
		track.Name, track.TrackId,
		len(track.LeftBoundary), len(track.RightBoundary))

	if len(track.LeftBoundary) == 0 || len(track.RightBoundary) == 0 {
		return fmt.Errorf("track has no boundary points")
	}

	// Use the shorter length to avoid index-out-of-range
	n := int(math.Min(float64(len(track.LeftBoundary)), float64(len(track.RightBoundary))))
	c.centerline = make([]Point, n)

	for i := 0; i < n; i++ {
		l := track.LeftBoundary[i]
		r := track.RightBoundary[i]
		c.centerline[i] = Point{
			X: (float64(l.X) + float64(r.X)) / 2,
			Y: (float64(l.Y) + float64(r.Y)) / 2,
		}
	}

	log.Printf("Centerline computed with %d points", len(c.centerline))
	return nil
}

func (c *CarClient) streamUpdates(ctx context.Context) error {
	stream, err := c.client.StreamRaceUpdates(ctx, &pb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to start stream: %v", err)
	}

	for {
		update, err := stream.Recv()
		if err == io.EOF {
			log.Println("Race update stream ended")
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream recv error: %v", err)
		}

		c.handleUpdate(update)
	}
}

func (c *CarClient) handleUpdate(update *pb.RaceUpdate) {
	// Update our own car state
	for _, car := range update.Cars {
		if car.CarId == carId {
			c.myCarState = car
			log.Printf("🏎️  %s | (%.1f, %.1f) | %.1f u/s | %.1f° | Lap %d",
				car.CarId, car.Position.X, car.Position.Y,
				car.Speed, car.Heading, car.Lap)
			break
		}
	}

	// Race status
	if update.RaceStatus != nil {
		st := update.RaceStatus
		if st.Status == "finished" {
			log.Printf("🏁 RACE FINISHED — time: %.2fs  (laps: %d)",
				float64(st.RaceTime)/1000.0, st.TotalLaps)
		} else if st.Status == "racing" {
			log.Printf("Race ongoing — time: %.2fs  laps: %d",
				float64(st.RaceTime)/1000.0, st.TotalLaps)
		}
	}
}

func (c *CarClient) sendInput(ctx context.Context, steering, throttle, brake float32, boost bool) error {
	c.sequence++ // local counter only — not sent to server

	ts := time.Now().UnixMilli()
	if ts > math.MaxInt32 {
		ts = math.MaxInt32 // very defensive
	}

	input := &pb.PlayerInput{
		CarId:     carId,
		AuthToken: authToken,
		Steering:  steering,
		Throttle:  throttle,
		Brake:     brake,
		Boost:     boost,
		Timestamp: int32(ts),
	}

	ack, err := c.client.SendPlayerInput(ctx, input)
	if err != nil {
		return fmt.Errorf("send input failed: %v", err)
	}

	if !ack.Accepted {
		log.Printf("Input rejected (loop %d): %s", ack.GameLoop, ack.Reason)
		return fmt.Errorf("rejected: %s", ack.Reason)
	}

	return nil
}

// ────────────────────────────────────────────────
// Basic centerline following (look-ahead steering)
// ────────────────────────────────────────────────
func (c *CarClient) getAIInput() (steering, throttle, brake float32, boost bool) {
	if c.myCarState == nil || len(c.centerline) == 0 {
		return 0, 0.6, 0, false // safe fallback
	}

	pos := c.myCarState.Position
	heading := float64(c.myCarState.Heading) // convert to float64

	// Find closest point on centerline (naive linear search)
	minDist := math.MaxFloat64
	closestIdx := 0
	for i, p := range c.centerline {
		dx := p.X - float64(pos.X)
		dy := p.Y - float64(pos.Y)
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < minDist {
			minDist = dist
			closestIdx = i
		}
	}

	// Look ahead several points
	targetIdx := (closestIdx + lookAheadPoints) % len(c.centerline)
	target := c.centerline[targetIdx]

	dx := target.X - float64(pos.X)
	dy := target.Y - float64(pos.Y)
	// distToTarget := math.Sqrt(dx*dx + dy*dy)  // removed since unused

	// Desired heading in degrees
	desiredHeading := math.Atan2(dy, dx) * 180 / math.Pi

	// Angle error (shortest direction, normalized -180..180)
	angleError := math.Mod(desiredHeading-heading+540, 360) - 180

	// Proportional steering
	steering = float32(angleError / 45.0) // tune divisor: smaller = sharper turns
	steering = max(-1, min(1, steering))

	// Throttle & brake logic
	throttle = 0.9
	brake = 0.0
	boost = false

	// Slow down when far off track or sharp correction needed
	if minDist > maxOffTrackDist || math.Abs(angleError) > 65 {
		throttle = 0.45
		brake = 0.3
	}

	// Conservative boost usage
	if minDist < 15 && math.Abs(angleError) < 15 && throttle > 0.75 {
		boost = true
	}

	return steering, throttle, brake, boost
}

func min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func main() {
	client, err := NewCarClient()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Load track geometry first
	if err := client.loadTrack(ctx); err != nil {
		log.Fatalf("Failed to load track: %v", err)
	}

	// Start background stream of race updates
	go func() {
		if err := client.streamUpdates(ctx); err != nil {
			log.Printf("Stream error: %v", err)
		}
	}()

	// Give a moment for initial data (check-in + first state)
	time.Sleep(800 * time.Millisecond)

	// Main control loop
	ticker := time.NewTicker(inputRate)
	defer ticker.Stop()

	log.Printf("AI driver started — Car %s — centerline follower", carId)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			steering, throttle, brake, boost := client.getAIInput()

			if err := client.sendInput(ctx, steering, throttle, brake, boost); err != nil {
				log.Printf("Input error: %v", err)
			}
		}
	}
}
