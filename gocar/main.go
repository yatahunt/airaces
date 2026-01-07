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

var carId string

const (
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
	raceType   pb.RaceType
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

// CheckIn - register with the server
func (c *CarClient) checkIn(ctx context.Context) error {
	resp, err := c.client.CheckIn(ctx, &pb.RegisterPlayer{
		CarId:      carId,
		PlayerName: "AI Driver " + carId,
		Password:   "", // Demo mode
	})
	if err != nil {
		return fmt.Errorf("CheckIn failed: %v", err)
	}

	if !resp.Accepted {
		return fmt.Errorf("registration rejected: %s", resp.Message)
	}

	log.Printf("✓ %s", resp.Message)
	if resp.IsSpectator {
		log.Printf("Logged in as spectator")
	}

	// Store race type
	c.raceType = resp.Race
	log.Printf("Race type: %s", c.raceType.String())

	// Load track from check-in response
	if resp.Track != nil {
		c.loadTrackFromInfo(resp.Track)
	}

	return nil
}

// Load track from TrackInfo (either from CheckIn or GetTrack)
func (c *CarClient) loadTrackFromInfo(track *pb.TrackInfo) {
	log.Printf("Loaded track: %s (%s) — %d left / %d right points",
		track.Name, track.TrackId,
		len(track.LeftBoundary), len(track.RightBoundary))

	if len(track.LeftBoundary) == 0 || len(track.RightBoundary) == 0 {
		log.Printf("Warning: track has no boundary points")
		return
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
}

// Fetch track boundaries and compute simple centerline (alternative method)
func (c *CarClient) loadTrack(ctx context.Context) error {
	track, err := c.client.GetTrack(ctx, &pb.Empty{})
	if err != nil {
		return fmt.Errorf("GetTrack failed: %v", err)
	}

	c.loadTrackFromInfo(track)
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
			statusStr := car.Status.String()
			log.Printf("🏎️  %s [%s] | (%.1f, %.1f) | %.1f u/s | %.1f° | Lap %d",
				car.CarId, statusStr, car.Position.X, car.Position.Y,
				car.Speed, car.Heading, car.Lap)
			break
		}
	}

	// Log penalties if any
	if len(update.Penalties) > 0 {
		for _, penalty := range update.Penalties {
			if penalty.CarId == carId {
				log.Printf("⚠️  PENALTY: %s (remaining: %.1fs)",
					penalty.Reason, float64(penalty.RemainingPenalty)/1000.0)
			}
		}
	}

	// Race status
	if update.RaceStatus != nil {
		st := update.RaceStatus
		if st.Status == "finished" {
			log.Printf("🏁 RACE FINISHED — tick: %d (laps: %d)",
				st.GameTick, st.TotalLaps)
		}
	}
}

func (c *CarClient) sendInput(ctx context.Context, steering, throttle, brake float32) error {
	c.sequence++ // local counter only — not sent to server

	ts := time.Now().UnixMilli()
	if ts > math.MaxInt32 {
		ts = math.MaxInt32 // very defensive
	}

	input := &pb.PlayerInput{
		CarId:     carId,
		AuthToken: getAuthToken(carId),
		Steering:  steering,
		Throttle:  throttle,
		Brake:     brake,
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
func (c *CarClient) getAIInput() (steering, throttle, brake float32) {
	if c.myCarState == nil || len(c.centerline) == 0 {
		return 0, 0.6, 0 // safe fallback
	}

	// Don't send input if serving penalty
	if c.myCarState.Status == pb.CarStatus_SERVINGPENALTY {
		return 0, 0, 1.0 // Full brake during penalty
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

	// Slow down when far off track or sharp correction needed
	if minDist > maxOffTrackDist || math.Abs(angleError) > 65 {
		throttle = 0.45
		brake = 0.3
	}

	return steering, throttle, brake
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
	carId = getCarLetter()
	client, err := NewCarClient()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	// Check in with server
	if err := client.checkIn(ctx); err != nil {
		log.Fatalf("Failed to check in: %v", err)
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
			steering, throttle, brake := client.getAIInput()

			if err := client.sendInput(ctx, steering, throttle, brake); err != nil {
				log.Printf("Input error: %v", err)
			}
		}
	}
}

func getAuthToken(carId string) string {
	return "demo-token-" + carId
}

func getCarLetter() string {
	letter := os.Getenv("CAR_LETTER")
	if letter == "" {
		log.Fatal("CAR_LETTER environment variable is not set")
	}

	log.Printf("car letter: %s", letter)
	return letter
}
