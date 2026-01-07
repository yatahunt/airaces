package main

import (
	"context"
	"encoding/csv"
	"log"
	"math"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	pb "server/proto" // your generated proto package

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	port             = ":50051"
	updateRate       = time.Second / 60 // 60 FPS
	numCars          = 5
	totalLaps        = 3
	observersallowed = true
	observersID      = "OBSERVER"
	observerstoken   = "OBSERVERTOKEN"
	maxSpeed         = float32(300.0)
	acceleration     = float32(200.0)
	brakeForce       = float32(400.0)
	friction         = float32(50.0)
	turnSpeed        = float32(180.0)
)

type TrackPoint struct {
	centerX    float32
	centerY    float32
	widthLeft  float32
	widthRight float32
}

type PlayerInput struct {
	steering  float32
	throttle  float32
	brake     float32
	timestamp int32
}

type CarInfo struct {
	carId  string
	power  float32
	weight float32
	x      float32
	y      float32
	z      float32
}

type CarServer struct {
	pb.UnimplementedCarServiceServer
	mu          sync.RWMutex
	carInfos    []CarInfo
	carStates   map[string]*pb.CarState
	playerInput map[string]*PlayerInput
	authTokens  map[string]string
	penalties   map[string]*pb.CarPenalty
	raceStatus  *pb.RaceStatus
	raceStarted time.Time
	gameTick    int32
	clients     map[chan *pb.RaceUpdate]struct{}
	track       *pb.TrackInfo
	raceType    pb.RaceType
}

func loadTrackFromCSV(filename string) (*pb.TrackInfo, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comment = '#'

	// Read all records
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Parse track points
	trackPoints := make([]TrackPoint, 0, len(records))
	for _, record := range records {
		if len(record) < 4 {
			continue
		}

		centerX, _ := strconv.ParseFloat(record[0], 32)
		centerY, _ := strconv.ParseFloat(record[1], 32)
		widthRight, _ := strconv.ParseFloat(record[2], 32)
		widthLeft, _ := strconv.ParseFloat(record[3], 32)

		trackPoints = append(trackPoints, TrackPoint{
			centerX:    float32(centerX),
			centerY:    float32(centerY),
			widthLeft:  float32(widthLeft),
			widthRight: float32(widthRight),
		})
	}

	if len(trackPoints) == 0 {
		log.Fatal("No track points loaded")
	}

	// Generate left and right boundaries
	leftBoundary := make([]*pb.Point3D, len(trackPoints))
	rightBoundary := make([]*pb.Point3D, len(trackPoints))

	for i := 0; i < len(trackPoints); i++ {
		point := trackPoints[i]

		// Calculate tangent vector (direction of track)
		var dx, dy float32
		if i == 0 {
			// Use direction to next point
			dx = trackPoints[i+1].centerX - point.centerX
			dy = trackPoints[i+1].centerY - point.centerY
		} else if i == len(trackPoints)-1 {
			// Use direction from previous point
			dx = point.centerX - trackPoints[i-1].centerX
			dy = point.centerY - trackPoints[i-1].centerY
		} else {
			// Average direction
			dx = trackPoints[i+1].centerX - trackPoints[i-1].centerX
			dy = trackPoints[i+1].centerY - trackPoints[i-1].centerY
		}

		// Normalize tangent
		length := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if length > 0 {
			dx /= length
			dy /= length
		}

		// Perpendicular vector (rotated 90 degrees)
		// Left is (-dy, dx), Right is (dy, -dx)
		perpX := -dy
		perpY := dx

		// Calculate boundary points
		leftBoundary[i] = &pb.Point3D{
			X: point.centerX + perpX*point.widthLeft,
			Y: point.centerY + perpY*point.widthLeft,
			Z: 0,
		}

		rightBoundary[i] = &pb.Point3D{
			X: point.centerX - perpX*point.widthRight,
			Y: point.centerY - perpY*point.widthRight,
			Z: 0,
		}
	}

	return &pb.TrackInfo{
		TrackId:       "hockenheim",
		Name:          "Hockenheim Circuit",
		LeftBoundary:  leftBoundary,
		RightBoundary: rightBoundary,
	}, nil
}

func NewCarServer() *CarServer {
	carInfos := make([]CarInfo, numCars)
	carStates := make(map[string]*pb.CarState)
	playerInput := make(map[string]*PlayerInput)
	authTokens := make(map[string]string)
	penalties := make(map[string]*pb.CarPenalty)

	// Load track from CSV
	track, err := loadTrackFromCSV("./tracks/hockenheim.csv")
	if err != nil {
		log.Fatalf("Failed to load track: %v", err)
	}

	// Start cars at first track point with staggered positions
	startX := track.LeftBoundary[0].X
	startY := track.LeftBoundary[0].Y

	for i := 0; i < numCars; i++ {
		carId := string(rune('A' + i))
		carInfos[i] = CarInfo{
			carId:  carId,
			power:  float32(80 + i*5),
			weight: float32(1000 + i*50),
			x:      startX,
			y:      startY + float32(i*10),
			z:      0.0,
		}

		carStates[carId] = &pb.CarState{
			CarId:  carId,
			Status: pb.CarStatus_WAITING,
			Position: &pb.Point3D{
				X: carInfos[i].x,
				Y: carInfos[i].y,
				Z: carInfos[i].z,
			},
			Heading: 0.0,
			Speed:   0.0,
			Lap:     0,
		}

		playerInput[carId] = &PlayerInput{}

		authTokens[carId] = "demo-token-" + carId
		log.Printf("Car %s auth token: %s", carId, authTokens[carId])
	}

	log.Printf("Loaded track '%s' with %d points", track.Name, len(track.LeftBoundary))

	s := &CarServer{
		carInfos:    carInfos,
		carStates:   carStates,
		playerInput: playerInput,
		authTokens:  authTokens,
		penalties:   penalties,
		raceStatus: &pb.RaceStatus{
			Status:    "racing",
			TotalLaps: totalLaps,
			GameTick:  0,
		},
		raceStarted: time.Now(),
		clients:     make(map[chan *pb.RaceUpdate]struct{}),
		gameTick:    0,
		track:       track,
		raceType:    pb.RaceType_RACEBYLAPS,
	}

	// Set all cars to RACING status
	for _, state := range carStates {
		state.Status = pb.CarStatus_RACING
	}

	go s.physicsLoop()

	return s
}

// GetTrack RPC - returns track information without authentication
func (s *CarServer) GetTrack(ctx context.Context, req *pb.Empty) (*pb.TrackInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.track, nil
}

// CheckIn RPC - handles player registration and returns static data
func (s *CarServer) CheckIn(ctx context.Context, req *pb.RegisterPlayer) (*pb.CheckInResponse, error) {
	carId := req.GetCarId()

	// Check if this is a spectator
	isSpectator := false
	token := ""

	// Validate if car exists
	s.mu.RLock()
	existingToken, exists := s.authTokens[carId]
	s.mu.RUnlock()

	if !exists && observersallowed && carId == observersID {
		token = observerstoken
		exists = true
		isSpectator = true
	} else if exists {
		token = existingToken
	}

	if !exists {
		return &pb.CheckInResponse{
			Accepted: false,
			Message:  "Car ID not found",
		}, nil
	}

	// In production, validate password here
	// For demo, we accept all check-ins

	s.mu.RLock()
	track := s.track
	raceType := s.raceType
	s.mu.RUnlock()

	message := "Welcome to the race!"
	if isSpectator {
		message = "Welcome spectator!"
	}

	return &pb.CheckInResponse{
		Accepted:    true,
		AuthToken:   token,
		Message:     message,
		IsSpectator: isSpectator,
		Track:       track,
		Race:        raceType,
	}, nil
}

// Validate auth token
func (s *CarServer) validateToken(carId, token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	expected, ok := s.authTokens[carId]
	return ok && token == expected
}

// Unary RPC for per-frame input
func (s *CarServer) SendPlayerInput(ctx context.Context, input *pb.PlayerInput) (*pb.InputAck, error) {
	carId := input.GetCarId()
	if !s.validateToken(carId, input.GetAuthToken()) {
		return &pb.InputAck{
			Accepted: false,
			Reason:   "invalid token",
			GameLoop: s.gameTick,
		}, nil
	}

	s.mu.Lock()
	s.playerInput[carId] = &PlayerInput{
		steering:  input.GetSteering(),
		throttle:  input.GetThrottle(),
		brake:     input.GetBrake(),
		timestamp: input.GetTimestamp(),
	}
	s.mu.Unlock()

	return &pb.InputAck{
		Accepted: true,
		GameLoop: s.gameTick,
	}, nil
}

// Stream race updates
func (s *CarServer) StreamRaceUpdates(req *pb.Empty, stream pb.CarService_StreamRaceUpdatesServer) error {
	clientChan := make(chan *pb.RaceUpdate, 10)

	s.mu.Lock()
	s.clients[clientChan] = struct{}{}
	s.mu.Unlock()

	// Stream updates
	defer func() {
		s.mu.Lock()
		delete(s.clients, clientChan)
		close(clientChan)
		s.mu.Unlock()
	}()

	for update := range clientChan {
		if err := stream.Send(update); err != nil {
			return err
		}
	}
	return nil
}

// Physics/game loop
func (s *CarServer) physicsLoop() {
	ticker := time.NewTicker(updateRate)
	defer ticker.Stop()
	last := time.Now()

	for range ticker.C {
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		last = now

		s.mu.Lock()
		s.gameTick++

		var maxLap int32 = 0
		var maxProgress float32 = 0

		for _, car := range s.carInfos {
			state := s.carStates[car.carId]
			input := s.playerInput[car.carId]

			// Update penalty timers
			if penalty, hasPenalty := s.penalties[car.carId]; hasPenalty {
				penalty.RemainingPenalty -= int32(dt * 1000) // Convert to milliseconds
				if penalty.RemainingPenalty <= 0 {
					delete(s.penalties, car.carId)
					state.Status = pb.CarStatus_RACING
				} else {
					state.Status = pb.CarStatus_SERVINGPENALTY
				}
			}

			// Only update physics if car is racing (not serving penalty or finished)
			if state.Status == pb.CarStatus_RACING {
				s.updateCarPhysics(state, input, dt)

				// Determine leader (by lap and progress along track)
				progress := s.calculateTrackProgress(state.Position)
				if state.Lap > maxLap || (state.Lap == maxLap && progress > maxProgress) {
					maxLap = state.Lap
					maxProgress = progress
				}

				// Check if finished
				if state.Lap >= totalLaps {
					state.Status = pb.CarStatus_FINISHED
				}
			}
		}

		s.raceStatus.GameTick = s.gameTick
		if maxLap >= totalLaps {
			s.raceStatus.Status = "finished"
		}

		// Create update
		update := s.createRaceUpdate()
		for clientChan := range s.clients {
			select {
			case clientChan <- update:
			default:
			}
		}

		s.mu.Unlock()
	}
}

// Calculate progress along track (0 to 1)
func (s *CarServer) calculateTrackProgress(pos *pb.Point3D) float32 {
	// Simple implementation: find closest track point
	minDist := float32(math.MaxFloat32)
	closestIdx := 0

	for i, leftPoint := range s.track.LeftBoundary {
		rightPoint := s.track.RightBoundary[i]
		centerX := (leftPoint.X + rightPoint.X) / 2
		centerY := (leftPoint.Y + rightPoint.Y) / 2

		dx := pos.X - centerX
		dy := pos.Y - centerY
		dist := dx*dx + dy*dy

		if dist < minDist {
			minDist = dist
			closestIdx = i
		}
	}

	return float32(closestIdx) / float32(len(s.track.LeftBoundary))
}

// Physics for each car
func (s *CarServer) updateCarPhysics(state *pb.CarState, input *PlayerInput, dt float32) {
	// Apply acceleration/brake
	if input.throttle > 0 {
		state.Speed += acceleration * input.throttle * dt
	} else if input.brake > 0 {
		state.Speed -= brakeForce * input.brake * dt
	} else {
		state.Speed -= friction * dt
	}

	if state.Speed < 0 {
		state.Speed = 0
	}
	if state.Speed > maxSpeed {
		state.Speed = maxSpeed
	}

	// Steering
	if state.Speed > 10 && input.steering != 0 {
		turnRate := turnSpeed * (state.Speed / maxSpeed)
		state.Heading += turnRate * input.steering * dt
		for state.Heading < 0 {
			state.Heading += 360
		}
		for state.Heading >= 360 {
			state.Heading -= 360
		}
	}

	rad := float64(state.Heading) * math.Pi / 180
	dx := float32(math.Cos(rad)) * state.Speed * dt
	dy := float32(math.Sin(rad)) * state.Speed * dt

	state.Position.X += dx
	state.Position.Y += dy

	// Simple lap detection (check if crossed start/finish line)
	// This is a placeholder - you'd want proper lap detection
	progress := s.calculateTrackProgress(state.Position)
	if progress < 0.1 && state.Speed > 0 {
		// Crossed finish line (detect lap completion logic here)
	}
}

func (s *CarServer) createRaceUpdate() *pb.RaceUpdate {
	states := make([]*pb.CarState, 0, len(s.carStates))
	for _, state := range s.carStates {
		states = append(states, &pb.CarState{
			CarId:  state.CarId,
			Status: state.Status,
			Position: &pb.Point3D{
				X: state.Position.X,
				Y: state.Position.Y,
				Z: state.Position.Z,
			},
			Heading: state.Heading,
			Speed:   state.Speed,
			Lap:     state.Lap,
		})
	}

	penalties := make([]*pb.CarPenalty, 0, len(s.penalties))
	for _, penalty := range s.penalties {
		penalties = append(penalties, &pb.CarPenalty{
			CarId:            penalty.CarId,
			Reason:           penalty.Reason,
			GameTick:         penalty.GameTick,
			RemainingPenalty: penalty.RemainingPenalty,
		})
	}

	return &pb.RaceUpdate{
		RaceStatus: &pb.RaceStatus{
			Status:    s.raceStatus.Status,
			TotalLaps: s.raceStatus.TotalLaps,
			GameTick:  s.raceStatus.GameTick,
		},
		Cars:      states,
		Penalties: penalties,
		GameTick:  s.gameTick,
	}
}

func main() {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	carServer := NewCarServer()

	pb.RegisterCarServiceServer(grpcServer, carServer)
	reflection.Register(grpcServer)

	log.Printf("🏎️  Racing server listening on %s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
