package main

import (
	"context"
	"log"
	"math"
	"net"
	"sync"
	"time"

	pb "server/proto" // your generated proto package

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	port        = ":50051"
	updateRate  = time.Second / 60 // 60 FPS
	trackWidth  = 1100.0
	trackHeight = 500.0
	numCars     = 5
	totalLaps   = 3

	maxSpeed        = float32(300.0)
	acceleration    = float32(200.0)
	brakeForce      = float32(400.0)
	friction        = float32(50.0)
	turnSpeed       = float32(180.0)
	boostMultiplier = float32(1.5)
)

type PlayerInput struct {
	steering  float32
	throttle  float32
	brake     float32
	boost     bool
	timestamp int64
	sequence  int32
}

type CarInfo struct {
	carId  string
	team   string
	power  float32
	color  string
	driver string
	x      float32
	y      float32
}

type CarServer struct {
	pb.UnimplementedCarServiceServer
	mu          sync.RWMutex
	carInfos    []CarInfo
	carStates   map[string]*pb.CarState
	playerInput map[string]*PlayerInput
	authTokens  map[string]string
	raceStatus  *pb.RaceStatus
	raceStarted time.Time
	gameTick    int64
	clients     map[chan *pb.RaceUpdate]struct{}
}

func NewCarServer() *CarServer {
	carInfos := make([]CarInfo, numCars)
	carStates := make(map[string]*pb.CarState)
	playerInput := make(map[string]*PlayerInput)
	authTokens := make(map[string]string)

	colors := []string{"#FF0000", "#00FF00", "#0000FF", "#FFFF00", "#FF00FF"}
	drivers := []string{"Alice", "Bob", "Charlie", "Diana", "Eve"}

	for i := 0; i < numCars; i++ {
		carId := string(rune('A' + i))
		carInfos[i] = CarInfo{
			carId:  carId,
			team:   "Team " + string(rune('1'+i)),
			power:  float32(80 + i*5),
			color:  colors[i],
			driver: drivers[i],
			x:      100.0,
			y:      50.0 + float32(i*80),
		}

		carStates[carId] = &pb.CarState{
			CarId:   carId,
			X:       carInfos[i].x,
			Y:       carInfos[i].y,
			Heading: 0.0,
			Speed:   0.0,
			Lap:     0,
		}

		playerInput[carId] = &PlayerInput{}

		authTokens[carId] = "demo-token-" + carId
		log.Printf("Car %s auth token: %s", carId, authTokens[carId])
	}

	s := &CarServer{
		carInfos:    carInfos,
		carStates:   carStates,
		playerInput: playerInput,
		authTokens:  authTokens,
		raceStatus: &pb.RaceStatus{
			Status:      "racing",
			TotalLaps:   totalLaps,
			RaceTime:    0,
			LeaderCarId: "A",
		},
		raceStarted: time.Now(),
		clients:     make(map[chan *pb.RaceUpdate]struct{}),
		gameTick:    0,
	}

	go s.physicsLoop()

	return s
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
			Accepted:   false,
			Reason:     "invalid token",
			ServerTime: time.Now().UnixMilli(),
			GameTick:   s.gameTick,
		}, nil
	}

	s.mu.Lock()
	s.playerInput[carId] = &PlayerInput{
		steering:  input.GetSteering(),
		throttle:  input.GetThrottle(),
		brake:     input.GetBrake(),
		boost:     input.GetBoost(),
		timestamp: input.GetTimestamp(),
		sequence:  input.GetSequence(),
	}
	s.mu.Unlock()

	return &pb.InputAck{
		Accepted:   true,
		ServerTime: time.Now().UnixMilli(),
		GameTick:   s.gameTick,
	}, nil
}

// Stream race updates
func (s *CarServer) StreamRaceUpdates(req *pb.Empty, stream pb.CarService_StreamRaceUpdatesServer) error {
	clientChan := make(chan *pb.RaceUpdate, 10)

	s.mu.Lock()
	s.clients[clientChan] = struct{}{}
	s.mu.Unlock()

	// Send check-in
	checkIn := make([]*pb.CarInfo, len(s.carInfos))
	for i, info := range s.carInfos {
		checkIn[i] = &pb.CarInfo{
			CarId:  info.carId,
			Team:   info.team,
			Power:  info.power,
			Color:  info.color,
			Driver: info.driver,
		}
	}

	if err := stream.Send(&pb.RaceUpdate{
		Update: &pb.RaceUpdate_CheckIn{
			CheckIn: &pb.CheckIn{
				Cars:      checkIn,
				Timestamp: time.Now().UnixNano(),
			},
		},
	}); err != nil {
		return err
	}

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

		var leader string
		maxLap := int32(0)
		maxX := float32(0)

		for _, car := range s.carInfos {
			state := s.carStates[car.carId]
			input := s.playerInput[car.carId]
			s.updateCarPhysics(state, input, dt)

			// Determine leader
			if state.Lap > maxLap || (state.Lap == maxLap && state.X > maxX) {
				maxLap = state.Lap
				maxX = state.X
				leader = state.CarId
			}
		}

		s.raceStatus.RaceTime = time.Since(s.raceStarted).Milliseconds()
		s.raceStatus.LeaderCarId = leader
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

// Physics for each car
func (s *CarServer) updateCarPhysics(state *pb.CarState, input *PlayerInput, dt float32) {
	// Apply acceleration/brake
	if input.throttle > 0 {
		acc := acceleration
		if input.boost {
			acc *= boostMultiplier
		}
		state.Speed += acc * input.throttle * dt
	} else if input.brake > 0 {
		state.Speed -= brakeForce * input.brake * dt
	} else {
		state.Speed -= friction * dt
	}

	if state.Speed < 0 {
		state.Speed = 0
	}
	maxS := maxSpeed
	if input.boost {
		maxS *= boostMultiplier
	}
	if state.Speed > maxS {
		state.Speed = maxS
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

	state.X += dx
	state.Y += dy

	// Wrap X and limit Y
	if state.X > trackWidth {
		state.X = 0
		state.Lap++
	}
	if state.X < 0 {
		state.X = trackWidth
	}
	if state.Y < 0 {
		state.Y = 0
	}
	if state.Y > trackHeight {
		state.Y = trackHeight
	}

	state.CurrentSteering = input.steering
	state.CurrentThrottle = input.throttle
	state.Timestamp = time.Now().UnixNano()
}

func (s *CarServer) createRaceUpdate() *pb.RaceUpdate {
	states := make([]*pb.CarState, 0, len(s.carStates))
	for _, state := range s.carStates {
		states = append(states, &pb.CarState{
			CarId:           state.CarId,
			X:               state.X,
			Y:               state.Y,
			Heading:         state.Heading,
			Speed:           state.Speed,
			Lap:             state.Lap,
			CurrentSteering: state.CurrentSteering,
			CurrentThrottle: state.CurrentThrottle,
			Timestamp:       state.Timestamp,
		})
	}

	return &pb.RaceUpdate{
		Update: &pb.RaceUpdate_RaceData{
			RaceData: &pb.RaceData{
				RaceStatus: &pb.RaceStatus{
					Status:      s.raceStatus.Status,
					TotalLaps:   s.raceStatus.TotalLaps,
					RaceTime:    s.raceStatus.RaceTime,
					LeaderCarId: s.raceStatus.LeaderCarId,
				},
				Cars:      states,
				Timestamp: time.Now().UnixNano(),
			},
		},
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
