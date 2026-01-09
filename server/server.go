package main

import (
	"context"
	"log"
	pb "server/proto"
	"time"
)

func NewCarServer() *CarServer {
	carInfos := make([]CarInfo, numCars)
	carStates := make(map[string]*CarStateExtended)
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

		carStates[carId] = &CarStateExtended{
			CarState: &pb.CarState{
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
			},
			lastProgress:    0.0,
			crossedFinish:   false,
			bestLapTime:     0.0,
			currentLapStart: time.Now(),
			lapTimes:        make([]float32, 0),
		}

		playerInput[carId] = &PlayerInput{}

		authTokens[carId] = "demo-token-" + carId
		log.Printf("Car %s auth token: %s", carId, authTokens[carId])
	}

	log.Printf("Loaded track '%s' with %d points", track.Name, len(track.LeftBoundary))

	// Set race type (can be configured)
	raceType := pb.RaceType_RACEBYLAPS
	raceLaps := int32(totalLaps)
	raceTimeRemaining := int32(0)

	if raceType == pb.RaceType_RACEBYTIME {
		raceTimeRemaining = raceTime
	}

	s := &CarServer{
		carInfos:    carInfos,
		carStates:   carStates,
		playerInput: playerInput,
		authTokens:  authTokens,
		penalties:   penalties,
		raceStatus: &pb.RaceStatus{
			Status:    "racing",
			TotalLaps: raceLaps,
			GameTick:  0,
		},
		raceStarted:  time.Now(),
		clients:      make(map[chan *pb.RaceUpdate]struct{}),
		gameTick:     0,
		track:        track,
		raceType:     raceType,
		raceLaps:     raceLaps,
		raceTimeLeft: raceTimeRemaining,
	}

	// Set all cars to RACING status
	for _, state := range carStates {
		state.Status = pb.CarStatus_RACING
		state.currentLapStart = time.Now()
	}

	go s.physicsLoop()

	return s
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
